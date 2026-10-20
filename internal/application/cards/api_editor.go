package cards

import (
	"context"
	"encoding/json"
)

// APIEditor 是仅供所属用户主动编辑的配置视图；秘密只在当前编辑会话中使用，不进入列表或日志。
type APIEditor struct {
	// URL 是请求目标地址。
	URL string
	// Method 是请求方法。
	Method string
	// TimeoutSeconds 是请求超时秒数。
	TimeoutSeconds int
	// ContentType 是正文编码类型。
	ContentType string
	// Headers、Params 和 Body 是完整 JSON 模板，允许所属用户编辑，不可记录到日志。
	Headers, Params, Body string
	// ResponsePath 是返回值提取路径。
	ResponsePath string
	// RetryEnabled 表示是否启用幂等重试。
	RetryEnabled bool
}

// ownedAPIConfig 先核对 userID 对 cardID 的所有权，再读取当前 API 模板；ctx 控制读取取消。
func (s *Service) ownedAPIConfig(ctx context.Context, userID, cardID int64) (string, error) {
	// summary、err 是不向调用方暴露秘密的归属查询结果。
	summary, err := s.Get(ctx, userID, cardID)
	if err != nil {
		return "", err
	}
	if summary.Type != "api" {
		return "", &ValidationError{Message: "只有 API 卡密支持此操作"}
	}
	// card、err 保存通过归属校验后的完整配置，并再次确认读取期间所有者没有变化。
	card, err := s.ownedCardFull(ctx, userID, cardID)
	if err != nil {
		return "", err
	}
	if card.Type != "api" {
		return "", &ValidationError{Message: "卡密类型已变化，请重新打开"}
	}
	return card.APIConfig, nil
}

// GetAPIEditor 为所属用户读取 cardID 的完整编辑视图，普通详情仍只返回摘要。
func (s *Service) GetAPIEditor(ctx context.Context, userID, cardID int64) (APIEditor, error) {
	// raw、err 保存权限核验后的模板及查询错误。
	raw, err := s.ownedAPIConfig(ctx, userID, cardID)
	if err != nil {
		return APIEditor{}, err
	}
	// config、err 保存经过校验的配置，错误不回显模板内容。
	config, err := ParseAPIConfig(raw)
	if err != nil {
		return APIEditor{}, &ValidationError{Message: "已保存的 API 配置无效"}
	}
	// headers、params、body 来自已经解码成功的 JSON 对象，重新编码不会失败。
	headers, _ := json.Marshal(config.Headers)
	// params 保存查询参数模板的 JSON 文本。
	params, _ := json.Marshal(config.Params)
	// body 保存请求正文模板的 JSON 文本。
	body, _ := json.MarshalIndent(config.Body, "", "  ")
	return APIEditor{URL: config.URL, Method: config.Method, TimeoutSeconds: config.Timeout, ContentType: config.ContentType,
		Headers: string(headers), Params: string(params), Body: string(body), ResponsePath: config.ResponsePath, RetryEnabled: config.RetryEnabled}, nil
}

// PrepareAPITest 合并 userID 所有的 cardID 已保存配置和 raw 草稿，不写数据库；零标识表示新增测试。
func (s *Service) PrepareAPITest(ctx context.Context, userID, cardID int64, raw string) (string, error) {
	// err 是当前调用者或应用装配的校验错误。
	if err := s.validateUser(userID); err != nil {
		return "", err
	}
	// existing 仅在已保存卡密测试时读取，不传到浏览器。
	var existing string
	if cardID != 0 {
		// err 是归属校验或模板读取错误，越权时不会调用远端。
		var err error
		existing, err = s.ownedAPIConfig(ctx, userID, cardID)
		if err != nil {
			return "", err
		}
	}
	// config、err 是与保存操作共用的保留、替换、清除合并结果。
	config, err := normalizeAPIConfig(raw, existing)
	if err != nil {
		return "", &ValidationError{Message: "API 配置无效，请检查请求头、正文及幂等键设置"}
	}
	return config, nil
}
