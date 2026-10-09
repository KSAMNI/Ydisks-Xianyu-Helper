package chat

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// identityResolverFunc 将本地函数适配为身份端口；请求上下文与非敏感定位值原样传给测试回调。
type identityResolverFunc func(context.Context, string, string) (Identity, error)

// Resolve 调用测试函数f，ctx负责取消，accountID和chatID定位会话；返回预置身份或失败。
func (f identityResolverFunc) Resolve(ctx context.Context, accountID, chatID string) (Identity, error) {
	return f(ctx, accountID, chatID)
}

// TestIdentityListRequestBudget 验证整页缺失资料在首次查询失败后只产生一个资料请求，缓存不被旧值写回。
func TestIdentityListRequestBudget(t *testing.T) {
	// calls统计实际资料端口调用；repository记录是否错误写回旧身份。
	var calls atomic.Int32
	// repository记录真实写入次数，缓存与失败路径不应重复保存旧快照。
	repository := &fakeRepository{}
	// failure模拟平台查询失败，服务不得据此遍历其余联系人。
	failure := errors.New("profile denied")
	// service使用计数替身而不访问真实平台；ctx、accountID、chatID不参与夹具结果。
	// service只替换身份端口，保留全部应用准入和缓存行为。
	service := NewWithIdentity(repository, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		calls.Add(1)
		return Identity{}, failure
	}))
	// sessions包含同账号200个独立的缺失资料会话。
	sessions := make([]Session, 200)
	// index为每个夹具构造不同的会话与对端身份。
	for index := range sessions {
		sessions[index] = Session{AccountID: "account", ChatID: fmt.Sprint(index), PeerUserID: fmt.Sprint(index + 1)}
	}
	// rows和err保留完整列表和首个外部失败，失败不应导致列表丢失。
	rows, err := service.RefreshSessionIdentities(context.Background(), "account", sessions)
	if !errors.Is(err, failure) || len(rows) != 200 || calls.Load() != 1 {
		t.Fatalf("rows=%d calls=%d failure=%t", len(rows), calls.Load(), errors.Is(err, failure))
	}
	if len(repository.updatedSessionSnapshot()) != 0 {
		t.Fatal("失败不得重写缓存资料")
	}
}

// TestIdentityExistingProfilesAvoidPlatform 验证已有完整资料的首次读取不会因进程缓存为空而重新扫描平台。
func TestIdentityExistingProfilesAvoidPlatform(t *testing.T) {
	// calls检测任何不必要的外部资料查询。
	var calls atomic.Int32
	// service保留真实应用编排，仅替换外部端口；回调定位参数对计数无影响。
	service := NewWithIdentity(&fakeRepository{}, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		calls.Add(1)
		return Identity{}, nil
	}))
	// session模拟数据库已经保存的完整非敏感身份。
	session := Session{AccountID: "account", ChatID: "chat", PeerUserID: "peer", PeerName: "原昵称", PeerAvatar: "avatar"}
	// result和err用于检查资料与成功语义都保持不变。
	result, err := service.ResolveSessionIdentity(context.Background(), session)
	if err != nil || result != session || calls.Load() != 0 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
}

// TestIdentityRiskCooldownAndProbe验证账号冷却、递增、到期单次探测以及成功后的缓存。
func TestIdentityRiskCooldownAndProbe(t *testing.T) {
	// clock使用原子Unix秒避免并发测试注入时间产生数据竞争。
	var clock atomic.Int64
	clock.Store(100000)
	// calls统计逻辑查询；failure可在顺序探测之间切换，非并发修改。
	var calls atomic.Int32
	// failure仅在两次已完成请求之间切换，不与回调并发修改。
	failure := error(ErrIdentityRisk)
	// service的回调只返回预设分类，不包含任何外部账号信息。
	service := NewWithIdentity(&fakeRepository{}, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		calls.Add(1)
		return Identity{PeerName: "有效昵称"}, failure
	}))
	service.identities.clock = func() time.Time { return time.Unix(clock.Load(), 0) }
	// session是每次自然访问发起探测的同一会话。
	session := Session{AccountID: "account", ChatID: "chat", PeerUserID: "peer"}
	// index和minutes定义四次连续风险及其精确冷却边界。
	for index, minutes := range []int64{5, 10, 20, 30, 30} {
		// err确认到期时允许一次新探测，并将风险原样保留。
		// err检查当前时间点的查询或取消结果，失败必须保留原分类。
		if _, err := service.ResolveSessionIdentity(context.Background(), session); !errors.Is(err, ErrIdentityRisk) {
			t.Fatalf("probe %d err=%v", index, err)
		}
		clock.Add(minutes*60 - 1)
		// retry代表冷却末秒内的密集访问，不能产生任何追加请求。
		for retry := 0; retry < 20; retry++ {
			// err若非空说明冷却内错误地进入了外部查询。
			// err检查当前时间点的查询或取消结果，失败必须保留原分类。
			if _, err := service.ResolveSessionIdentity(context.Background(), Session{AccountID: "account", ChatID: fmt.Sprint(retry), PeerUserID: "peer"}); err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != int32(index+1) {
			t.Fatalf("冷却内调用=%d", calls.Load())
		}
		clock.Add(1)
	}
	failure = nil
	// err检查当前时间点的查询或取消结果，失败必须保留原分类。
	if _, err := service.ResolveSessionIdentity(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if service.identities.accounts["account"].risks != 0 {
		t.Fatal("成功必须重置风险次数")
	}
	// cached即使调用方仍持有缺失资料的旧快照，也应得到成功查询后的资料。
	cached, err := service.ResolveSessionIdentity(context.Background(), session)
	if err != nil || cached.PeerName != "有效昵称" || calls.Load() != 6 {
		t.Fatalf("cached=%+v calls=%d err=%v", cached, calls.Load(), err)
	}
}

// TestIdentityConcurrentAccountGate验证同账号列表和历史共享在途占用，不同账号独立且间隔生效。
func TestIdentityConcurrentAccountGate(t *testing.T) {
	// entered和release由主测试分别接收、关闭，唯一阻塞请求通过finished回收。
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// calls和clock可被并发请求安全访问。
	var calls, clock atomic.Int64
	clock.Store(100000)
	// service只阻塞首个账号的首次查询，其他准入查询直接成功。
	service := NewWithIdentity(&fakeRepository{}, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return Identity{}, nil
	}))
	service.identities.clock = func() time.Time { return time.Unix(clock.Load(), 0) }
	// session保存竞争请求共享的账号身份，chatID可变化但不能绕过账号锁。
	session := Session{AccountID: "first", ChatID: "chat", PeerUserID: "peer"}
	// 首个请求由测试拥有，释放后通过finished等待，不留下后台goroutine。
	go func() { defer close(finished); _, _ = service.ResolveSessionIdentity(context.Background(), session) }()
	<-entered
	// workers等待同时到来的列表请求全部退回缓存。
	var workers sync.WaitGroup
	// index发起多个HTTP级竞争替身；每个请求都有独立会话但同一账号。
	for index := 0; index < 40; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, _ = service.RefreshSessionIdentities(context.Background(), "first", []Session{session})
		}()
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("并发绕过准入=%d", calls.Load())
	}
	// err检查当前时间点的查询或取消结果，失败必须保留原分类。
	if _, err := service.ResolveSessionIdentity(context.Background(), Session{AccountID: "second", ChatID: "chat", PeerUserID: "peer"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("不同账号不应相互阻塞")
	}
	close(release)
	<-finished
	session.ChatID = "another"
	_, _ = service.ResolveSessionIdentity(context.Background(), session)
	if calls.Load() != 2 {
		t.Fatal("完成后仍须遵守开始间隔")
	}
	clock.Add(5)
	_, _ = service.ResolveSessionIdentity(context.Background(), session)
	if calls.Load() != 3 {
		t.Fatal("正常间隔到期应允许查询")
	}
}

// TestIdentityCacheExpiryAndPeerIsolation验证成功空字段负缓存、对端隔离及真实到期，不把旧快照覆盖新数据库资料。
func TestIdentityCacheExpiryAndPeerIsolation(t *testing.T) {
	// now在顺序测试中显式推进一天，避免真实等待。
	now := time.Unix(100000, 0)
	// calls计数不同缓存键是否真正查询；identity模拟先取得旧昵称、后取得新资料。
	calls := 0
	// identity是当前端口预置的成功资料，空头像也应缓存。
	identity := Identity{PeerName: "已查询昵称"}
	// repository记录写入参数，防止旧昵称或对端标识被重新提交。
	// repository记录真实写入次数，缓存与失败路径不应重复保存旧快照。
	repository := &fakeRepository{}
	// service只替换身份端口，保留全部应用准入和缓存行为。
	service := NewWithIdentity(repository, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) { calls++; return identity, nil }))
	service.identities.clock = func() time.Time { return now }
	// session只有已知对端标识，资料通过首次请求获取。
	session := Session{AccountID: "account", ChatID: "chat", PeerUserID: "one"}
	_, _ = service.ResolveSessionIdentity(context.Background(), session)
	now = now.Add(time.Hour)
	// newer代表数据库中比本次请求快照更新的昵称，内存缓存只能补缺不能覆盖。
	newer := session
	newer.PeerName = "数据库新昵称"
	// got和err检查缓存不能覆盖并发写入的数据库新昵称。
	if got, err := service.ResolveSessionIdentity(context.Background(), newer); err != nil || got.PeerName != newer.PeerName {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if calls != 1 {
		t.Fatal("合法空头像必须负缓存")
	}
	session.PeerUserID = "two"
	identity = Identity{}
	_, _ = service.ResolveSessionIdentity(context.Background(), session)
	if calls != 2 {
		t.Fatal("新对端不能复用旧资料")
	}
	now = now.Add(identityTTL)
	_, _ = service.ResolveSessionIdentity(context.Background(), session)
	if calls != 3 {
		t.Fatal("到期自然访问必须能够再次补查")
	}
	// updates只应包含首次非空成功资料，合法空结果和缓存命中不能写旧字段。
	updates := repository.updatedSessionSnapshot()
	if len(updates) != 1 || updates[0].PeerUserID != "" || updates[0].PeerAvatar != "" {
		t.Fatalf("updates=%+v", updates)
	}
}

// TestIdentityFailureCancellationAndPersistence验证取消、超时、存储失败及风险伴随取消时的独立分类。
func TestIdentityFailureCancellationAndPersistence(t *testing.T) {
	// testCase分别指定外部失败、期待退避及是否加入持久化错误。
	for _, testCase := range []struct {
		// name是可读场景名。
		name string
		// failure是资料端口返回的失败；nil表示取得新昵称。
		failure error
		// seconds是该结果应设置的下一次准入间隔。
		seconds int64
	}{{"cancel", context.Canceled, 5}, {"timeout", context.DeadlineExceeded, 60}, {"network", errors.New("offline"), 60}, {"risk_canceled", errors.Join(context.Canceled, ErrIdentityRisk), 300}, {"persist", nil, 5}} {
		t.Run(testCase.name, func(t *testing.T) {
			// now和repository用于检查真实完成后的占用以及持久化错误未被伪装。
			now := time.Unix(100000, 0)
			// repository模拟资料缓存写入失败，不输出任何存储敏感值。
			repository := &fakeRepository{updateErr: errors.New("storage unavailable")}
			// service的回调同时断言可选资料请求预算小于等于三秒。
			// service只替换身份端口，保留全部应用准入和缓存行为。
			service := NewWithIdentity(repository, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
				// deadline和ok确认所有入口都设置统一查询超时。
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > identityTimeout {
					t.Error("缺少三秒资料请求预算")
				}
				return Identity{PeerName: "新资料"}, testCase.failure
			}))
			service.identities.clock = func() time.Time { return now }
			// session携带旧昵称而无头像，仍有补齐必要。
			session := Session{AccountID: "account", ChatID: "chat", PeerUserID: "peer", PeerName: "旧资料"}
			// got和err保存前端可见资料及真实失败。
			got, err := service.ResolveSessionIdentity(context.Background(), session)
			if testCase.failure == nil {
				if !errors.Is(err, ErrIdentityPersist) || got.PeerName != "新资料" {
					t.Fatalf("persist result=%+v err=%v", got, err)
				}
			} else if !errors.Is(err, testCase.failure) || got.PeerName != "旧资料" {
				t.Fatalf("failure result=%+v err=%v", got, err)
			}
			// account在同步调用返回后可安全检查，busy必须释放且退避分类准确。
			account := service.identities.accounts["account"]
			if account.busy || account.nextAllowed.Sub(now) != time.Duration(testCase.seconds)*time.Second {
				t.Fatalf("account=%+v", account)
			}
		})
	}
}

// TestIdentityCapacityAndValidation验证容量不足时保守跳过、冷却不可淘汰、空闲清理及跨账号快照拒绝。
func TestIdentityCapacityAndValidation(t *testing.T) {
	// now是容量淘汰用可控时间；calls确认非法或满容量请求没有读取平台。
	now, calls := time.Unix(100000, 0), 0
	// service以小容量触发生产容量保护，风险响应保持冷却状态。
	service := NewWithIdentity(&fakeRepository{}, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		calls++
		return Identity{}, ErrIdentityRisk
	}))
	service.identities.capacity = 1
	service.identities.clock = func() time.Time { return now }
	// session是占满一个账号和一个身份缓存槽位的会话。
	session := Session{AccountID: "one", ChatID: "chat", PeerUserID: "peer"}
	_, _ = service.ResolveSessionIdentity(context.Background(), session)
	_, _ = service.ResolveSessionIdentity(context.Background(), Session{AccountID: "two", ChatID: "other", PeerUserID: "peer"})
	if calls != 1 || len(service.identities.accounts) != 1 || len(service.identities.entries) != 1 {
		t.Fatal("容量保护不能丢弃冷却")
	}
	now = now.Add(2 * identityTTL)
	_, _ = service.ResolveSessionIdentity(context.Background(), Session{AccountID: "two", ChatID: "other", PeerUserID: "peer"})
	if calls != 2 || len(service.identities.accounts) != 1 {
		t.Fatal("空闲状态应可清理")
	}
	// invalidRows在合法首行后加入不同归属，必须整批验证后才可能发请求。
	invalidRows := []Session{session, {AccountID: "two", ChatID: "chat"}}
	// err验证跨账号快照在任何外部请求之前被拒绝。
	if _, err := service.RefreshSessionIdentities(context.Background(), "one", invalidRows); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("跨账号快照未拒绝")
	}
	// ctx和cancel模拟请求在开始前已取消。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// err检查当前时间点的查询或取消结果，失败必须保留原分类。
	if _, err := service.ResolveSessionIdentity(ctx, session); !errors.Is(err, context.Canceled) {
		t.Fatal("取消应直接传播")
	}
	if calls != 2 {
		t.Fatal("非法或取消请求不得访问平台")
	}
}

// TestIdentityCancelledEntryCannotBypassAccountCapacity验证取消缓存可清理时，仍活跃的账号限额不能被新账号绕过。
func TestIdentityCancelledEntryCannotBypassAccountCapacity(t *testing.T) {
	// now和calls记录取消后立即切换账号的受限行为。
	now, calls := time.Unix(100000, 0), 0
	// service故意返回取消，既不触发风险，也不强占另一个账号的容量。
	service := NewWithIdentity(&fakeRepository{}, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		calls++
		return Identity{}, context.Canceled
	}))
	service.identities.clock = func() time.Time { return now }
	service.identities.capacity = 1
	_, _ = service.ResolveSessionIdentity(context.Background(), Session{AccountID: "first", ChatID: "chat", PeerUserID: "peer"})
	now = now.Add(identityGap)
	_, _ = service.ResolveSessionIdentity(context.Background(), Session{AccountID: "second", ChatID: "chat", PeerUserID: "peer"})
	if calls != 1 || len(service.identities.accounts) != 1 || len(service.identities.entries) != 0 {
		t.Fatal("活动账号容量不可通过清理空缓存绕过")
	}
}

// TestIdentitySuccessfulListBudgetAndCachePrefix验证200个已缓存会话不消耗平台预算，缺失资料一次只补一个且不修改输入。
func TestIdentitySuccessfulListBudgetAndCachePrefix(t *testing.T) {
	// now和calls分别控制请求间隔与逻辑平台计数。
	now, calls := time.Unix(100000, 0), 0
	// service返回完整资料，证明预算不是依靠首次失败才终止。
	service := NewWithIdentity(&fakeRepository{}, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		calls++
		return Identity{PeerName: "新昵称", PeerAvatar: "新头像"}, nil
	}))
	service.identities.clock = func() time.Time { return now }
	// sessions前200条已有完整资料，后两条依次需要查询。
	sessions := make([]Session, 202)
	// index为每条会话配置独立标识，不能通过同一缓存键制造虚假零查询。
	for index := range sessions {
		sessions[index] = Session{AccountID: "account", ChatID: fmt.Sprint(index), PeerUserID: fmt.Sprint(index), PeerName: "已有昵称", PeerAvatar: "已有头像"}
	}
	// err确认整页缓存仅回填，不产生资料请求。
	if _, err := service.RefreshSessionIdentities(context.Background(), "account", sessions[:200]); err != nil || calls != 0 {
		t.Fatalf("cached calls=%d err=%v", calls, err)
	}
	sessions[200].PeerName, sessions[200].PeerAvatar = "", ""
	sessions[201].PeerName, sessions[201].PeerAvatar = "", ""
	// updated和err保留仅第一条缺失会话被补齐的完整列表。
	updated, err := service.RefreshSessionIdentities(context.Background(), "account", sessions)
	if err != nil || calls != 1 || updated[200].PeerName != "新昵称" || updated[201].PeerName != "" || sessions[200].PeerName != "" {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	now = now.Add(identityGap)
	updated, err = service.RefreshSessionIdentities(context.Background(), "account", sessions)
	if err != nil || calls != 2 || updated[200].PeerAvatar != "新头像" || updated[201].PeerName != "新昵称" {
		t.Fatalf("next calls=%d err=%v", calls, err)
	}
}

// TestIdentityRequestCancellationReleasesSlot验证取消确实传入在途资料调用，返回后无遗留占用且其他入口仍可工作。
func TestIdentityRequestCancellationReleasesSlot(t *testing.T) {
	// started和finished由唯一测试请求发送，主测试负责取消并等待完成。
	started, finished := make(chan struct{}), make(chan error, 1)
	// service在资料端口等待ctx取消，不使用真实网络或三秒等待。
	service := NewWithIdentity(&fakeRepository{}, identityResolverFunc(func(ctx context.Context, accountID, chatID string) (Identity, error) {
		close(started)
		<-ctx.Done()
		return Identity{}, ctx.Err()
	}))
	// ctx和cancel模拟HTTP客户端中断，必须最终回收本次查询。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// session是单次阻塞请求的非敏感定位值。
	session := Session{AccountID: "account", ChatID: "chat", PeerUserID: "peer"}
	// 请求goroutine由本测试拥有，通过finished完成Join，避免取消测试自身泄漏工作器。
	go func() {
		// err是客户端取消后的资料查询结果，回传给主测试断言。
		_, err := service.ResolveSessionIdentity(ctx, session)
		finished <- err
	}()
	<-started
	cancel()
	// err确认父取消传播到端口并且释放共享占用。
	if err := <-finished; !errors.Is(err, context.Canceled) || service.identities.accounts["account"].busy {
		t.Fatalf("cancel err=%v", err)
	}
}
