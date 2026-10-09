package chat

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// ResolveSessionIdentity按需补全session展示身份，ctx继承调用方取消；返回可展示会话及真实查询或保存失败。
// s中的账号准入覆盖列表和历史入口；命中缓存、风控冷却或并发占用时不访问平台也不写回旧资料。
func (s *Service) ResolveSessionIdentity(ctx context.Context, session Session) (Session, error) {
	// result、err和attempted分别保存展示值、查询错误及是否占用平台预算，单会话入口无需返回预算。
	result, err, _ := s.resolveIdentity(ctx, session)
	return result, err
}

// resolveIdentity为ctx内的session执行一次有预算的资料补查，返回展示值、失败及实际请求标记。
func (s *Service) resolveIdentity(ctx context.Context, session Session) (Session, error, bool) {
	if s == nil || s.repository == nil || strings.TrimSpace(session.AccountID) == "" || strings.TrimSpace(session.ChatID) == "" {
		return session, ErrInvalidInput, false
	}
	if ctx.Err() != nil {
		return session, ctx.Err(), false
	}
	if session.PeerUserID == "1400" || s.identityResolver == nil {
		return session, nil, false
	}
	// cached和admitted是锁内计算的安全展示值及一次性准入；被拒绝时禁止任何外部I/O。
	cached, admitted := s.identities.begin(session)
	if !admitted {
		return cached, nil, false
	}
	// queryCtx和cancel将资料请求限制为父请求生命周期内最多三秒。
	queryCtx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()
	// identity和queryErr保存适配器完成Cookie收口后的平台展示结果，不含凭证。
	identity, queryErr := s.identityResolver.Resolve(queryCtx, session.AccountID, session.ChatID)
	identity.PeerName, identity.PeerAvatar = strings.TrimSpace(identity.PeerName), strings.TrimSpace(identity.PeerAvatar)
	// cooldown和recovered记录锁内状态变化，日志只在锁外写非敏感定位信息。
	cooldown, recovered := s.identities.finish(session, identity, queryErr)
	if cooldown > 0 {
		slog.WarnContext(ctx, "聊天资料查询进入风控冷却", "account", session.AccountID, "cooldown_seconds", int64(cooldown.Seconds()))
	}
	if recovered {
		slog.InfoContext(ctx, "聊天资料查询风控冷却后恢复", "account", session.AccountID)
	}
	if queryErr != nil {
		return session, queryErr, true
	}
	if identity.PeerName != "" {
		session.PeerName = identity.PeerName
	}
	if identity.PeerAvatar != "" {
		session.PeerAvatar = identity.PeerAvatar
	}
	// repository和supported限定成功资料的窄写入能力；只提交平台实际返回的字段，不能重写旧快照或对端标识。
	if repository, supported := s.repository.(SessionRepository); supported && (identity.PeerName != "" || identity.PeerAvatar != "") {
		// persistErr仅标记本地缓存未完成，已取回的展示结果继续返回给页面。
		if persistErr := repository.UpdateSessionIdentity(ctx, session.AccountID, session.ChatID, "", identity.PeerName, identity.PeerAvatar); persistErr != nil {
			return session, errors.Join(ErrIdentityPersist, persistErr), true
		}
	}
	return session, nil, true
}

// RefreshSessionIdentities对accountID所属sessions最多执行一次资料补查；ctx取消立即停止，失败保留整页旧资料。
// 已有资料先读缓存，未获准入不排队；所有成员先验证归属，避免错误快照触发其他账号请求。
func (s *Service) RefreshSessionIdentities(ctx context.Context, accountID string, sessions []Session) ([]Session, error) {
	accountID = strings.TrimSpace(accountID)
	if s == nil || s.repository == nil || accountID == "" {
		return sessions, ErrInvalidInput
	}
	// session验证整页账号及会话身份，校验结束前不能执行外部动作。
	for _, session := range sessions {
		if session.AccountID != accountID || strings.TrimSpace(session.ChatID) == "" {
			return sessions, ErrInvalidInput
		}
	}
	// result复制输入，避免改写调用方持有的缓存切片。
	result := append([]Session(nil), sessions...)
	// index和session定位本页待补齐的会话，第一次实际请求之后立即返回。
	for index, session := range result {
		// updated、err和attempted保存一次按需补查的结果、错误及预算消耗。
		updated, err, attempted := s.resolveIdentity(ctx, session)
		result[index] = updated
		if err != nil || attempted {
			return result, err
		}
	}
	return result, nil
}
