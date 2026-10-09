package chat

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrIdentityRisk 表示仅联系人资料查询遭到风控，不代表账号Session过期。
var ErrIdentityRisk = errors.New("联系人资料查询触发风控")

// ErrIdentityPersist 表示资料已取回但未成功写入本地，展示仍可使用当前结果。
var ErrIdentityPersist = errors.New("联系人资料缓存保存失败")

// identityTTL是成功资料及合法空字段的查询有效期，不使用会话消息更新时间替代。
const identityTTL = 24 * time.Hour

// identityGap限制同账号两个资料用例的开始间隔；内部Token换签仍遵循原有有界协议。
const identityGap = 5 * time.Second

// identityTimeout约束可选资料查询，不能拖住整个聊天列表。
const identityTimeout = 3 * time.Second

// identityCapacity限制每个服务实例的账号状态及身份缓存条目，满额时跳过补查。
const identityCapacity = 4096

// identityKey隔离不同账号、会话及对端，防止同名会话或身份变化复用旧资料。
type identityKey struct {
	// accountID是会话所属账号，不含任何凭证。
	accountID string
	// chatID是平台会话标识。
	chatID string
	// peerID是已知对端身份，变化后不得使用原对端缓存。
	peerID string
}

// identityEntry缓存安全展示值及有效期，空字段同样具有查询有效期。
type identityEntry struct {
	// value仅保存平台成功返回的昵称头像，初始化的历史缓存不复制旧快照。
	value Identity
	// validUntil表示允许再次按需查询的最早时刻。
	validUntil time.Time
}

// identityAccount限制账号资料请求；所有字段仅在identityPolicy.mu保护下访问。
type identityAccount struct {
	// busy由取得准入的同步请求持有，完成后释放，不创建后台工作器。
	busy bool
	// nextAllowed约束正常间隔、网络退避和风控冷却。
	nextAllowed time.Time
	// risks记录连续风险失败次数，只有成功平台结果才重置。
	risks int
	// touched用于惰性清理长时间不再访问且没有在途请求的账号。
	touched time.Time
}

// identityPolicy由单个聊天Service拥有，可并发使用，零值即可运行。
// mu只保护下述map及其内容，绝不跨凭证读取、数据库、网络或日志I/O持有；无goroutine或关闭职责。
type identityPolicy struct {
	// mu保护账号准入和资料缓存，外部调用前必须释放。
	mu sync.Mutex
	// accounts保存进程内共享的账号级准入，不提供跨进程限流。
	accounts map[string]*identityAccount
	// entries保存有界的非敏感资料及查询有效期。
	entries map[identityKey]identityEntry
	// clock仅供构造后的首次调用前注入测试时钟，生产默认time.Now；使用后禁止修改。
	clock func() time.Time
	// capacity仅供首次使用前配置更小测试容量，非正值使用固定生产上限。
	capacity int
}

// now返回策略时刻；p的可选时钟必须在并发使用前配置完成。
func (p *identityPolicy) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// begin尝试为session取得资料请求准入，并用新鲜缓存填补缺失字段；返回false时不得访问凭证或平台。
func (p *identityPolicy) begin(session Session) (Session, bool) {
	// now是本次原子准入判断使用的统一时刻；key限定缓存身份范围。
	now, key := p.now(), identityKey{session.AccountID, session.ChatID, session.PeerUserID}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.accounts == nil {
		p.accounts = make(map[string]*identityAccount)
		p.entries = make(map[identityKey]identityEntry)
	}
	// entry和exists保留缓存记录；新鲜缓存仅补缺失字段，不能覆盖数据库更新的展示值。
	entry, exists := p.entries[key]
	if exists && now.Before(entry.validUntil) {
		if session.PeerName == "" {
			session.PeerName = entry.value.PeerName
		}
		if session.PeerAvatar == "" {
			session.PeerAvatar = entry.value.PeerAvatar
		}
		return session, false
	}
	// limit是账号及缓存各自独立的最大容量。
	limit := p.capacity
	if limit <= 0 {
		limit = identityCapacity
	}
	if len(p.entries) >= limit || len(p.accounts) >= limit {
		p.prune(now)
	}
	if !exists && len(p.entries) >= limit {
		return session, false
	}
	if !exists && session.PeerName != "" && session.PeerAvatar != "" {
		p.entries[key] = identityEntry{validUntil: now.Add(identityTTL)}
		return session, false
	}
	// account和known定位共享准入；满容量时不得通过临时账号状态绕过保护。
	account, known := p.accounts[session.AccountID]
	if !known {
		if len(p.accounts) >= limit {
			return session, false
		}
		account = &identityAccount{}
		p.accounts[session.AccountID] = account
	}
	account.touched = now
	if account.busy || now.Before(account.nextAllowed) {
		return session, false
	}
	account.busy = true
	account.nextAllowed = now.Add(identityGap)
	p.entries[key] = entry
	return session, true
}

// prune仅由持有mu的begin调用；now用于淘汰过期一整天的资料和空闲账号，禁止清除在途或冷却保护。
func (p *identityPolicy) prune(now time.Time) {
	// key和entry遍历过期缓存；对应账号仍在请求中时保留当前请求占用。
	for key, entry := range p.entries {
		// account是当前缓存所属账号，可不存在于纯历史资料缓存中。
		account := p.accounts[key.accountID]
		if now.After(entry.validUntil.Add(identityTTL)) && (account == nil || !account.busy && !now.Before(account.nextAllowed)) {
			delete(p.entries, key)
		}
	}
	// id和account定位超过一天未使用且没有在途或冷却的准入状态。
	for id, account := range p.accounts {
		if !account.busy && !now.Before(account.nextAllowed) && now.After(account.touched.Add(identityTTL)) {
			delete(p.accounts, id)
		}
	}
}

// finish释放session的唯一在途占用，value和queryErr为真实平台结果；返回冷却时长与是否从风险状态恢复。
// 风险优先于取消，避免已经收到的风险因浏览器断开而遗失；此函数不进行外部I/O。
func (p *identityPolicy) finish(session Session, value Identity, queryErr error) (time.Duration, bool) {
	// now和key绑定本次平台完成时刻及缓存身份。
	now, key := p.now(), identityKey{session.AccountID, session.ChatID, session.PeerUserID}
	p.mu.Lock()
	defer p.mu.Unlock()
	// account必须来自成功begin，busy保护其不被惰性清理。
	account := p.accounts[session.AccountID]
	account.busy = false
	account.touched = now
	if errors.Is(queryErr, ErrIdentityRisk) {
		account.risks = min(account.risks+1, 4)
		// cooldown按连续失败递增，半小时封顶；到期只允许后续自然访问探测。
		cooldown := min(5*time.Minute*time.Duration(1<<(account.risks-1)), 30*time.Minute)
		account.nextAllowed = now.Add(cooldown)
		p.entries[key] = identityEntry{validUntil: account.nextAllowed}
		return cooldown, false
	}
	if queryErr != nil {
		if !errors.Is(queryErr, context.Canceled) {
			account.nextAllowed = now.Add(time.Minute)
			p.entries[key] = identityEntry{validUntil: account.nextAllowed}
		}
		return 0, false
	}
	// recovered说明本次成功终结了此前风险，便于锁外记录状态变化。
	recovered := account.risks > 0
	account.risks = 0
	p.entries[key] = identityEntry{value: value, validUntil: now.Add(identityTTL)}
	return 0, recovered
}
