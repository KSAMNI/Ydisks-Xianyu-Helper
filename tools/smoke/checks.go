package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// smokeClient 是带 Cookie 会话的冒烟 HTTP 客户端。
type smokeClient struct {
	// base 是服务根地址。
	base string
	// http 保存会话 Cookie，模拟浏览器登录态。
	http *http.Client
}

// newSmokeClient 创建独立 Cookie 罐的客户端。
func newSmokeClient(base string) *smokeClient {
	// jar 保存本次冒烟的会话 Cookie。
	jar, _ := cookiejar.New(nil)
	return &smokeClient{base: base, http: &http.Client{Timeout: 15 * time.Second, Jar: jar}}
}

// do 发送请求并返回状态码与响应体；body 非 nil 时按 JSON 发送。
func (c *smokeClient) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	// reader 是请求体；无请求体时为 nil。
	var reader io.Reader
	if body != nil {
		// payload 是序列化后的 JSON 请求体。
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(payload)
	}
	// req 是待发送的请求。
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// resp 是服务响应。
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	// data 是完整响应体。
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

// expectStatus 请求 path 并断言状态码，失败时错误中包含步骤名与响应片段。
func (c *smokeClient) expectStatus(ctx context.Context, step, method, path string, body any, want int) ([]byte, error) {
	// status、data 是响应状态码与响应体。
	status, data, err := c.do(ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", step, err)
	}
	if status != want {
		return nil, fmt.Errorf("%s: %s %s 返回 %d，期望 %d：%s", step, method, path, status, want, snippet(data))
	}
	return data, nil
}

// snippet 截取响应体前 200 个字节用于错误信息，避免输出过长。
func snippet(data []byte) string {
	if len(data) > 200 {
		return string(data[:200]) + "…"
	}
	return string(data)
}

// sessionState 是 /api/v1/session 响应中冒烟关心的字段。
type sessionState struct {
	// Authenticated 表示当前 Cookie 是否已登录。
	Authenticated bool `json:"authenticated"`
	// Initialized 表示系统是否已完成管理员初始化。
	Initialized bool `json:"initialized"`
	// Username 是已登录用户名。
	Username string `json:"username"`
}

// session 读取并解析当前会话状态。
func (c *smokeClient) session(ctx context.Context, step string) (sessionState, error) {
	// data 是会话响应体。
	data, err := c.expectStatus(ctx, step, http.MethodGet, "/api/v1/session", nil, http.StatusOK)
	if err != nil {
		return sessionState{}, err
	}
	// state 是解析后的会话状态。
	var state sessionState
	// err 保存会话响应的 JSON 解析错误，非空说明服务返回了非法响应。
	if err := json.Unmarshal(data, &state); err != nil {
		return sessionState{}, fmt.Errorf("%s: 响应不是合法 JSON: %w", step, err)
	}
	return state, nil
}

// listEndpoints 是登录后必须返回 200 JSON 的核心列表接口。
var listEndpoints = []string{"/api/v1/accounts", "/api/v1/cards", "/api/v1/orders", "/api/v1/items"}

// runChecks 对已启动的服务依次执行冒烟检查，遇到第一个失败即返回。
// password 是首次初始化并用于后续登录的管理员密码。
func runChecks(ctx context.Context, base, password string) error {
	// c 是带会话 Cookie 的客户端。
	c := newSmokeClient(base)
	// health 是健康检查响应体，必须声明数据库可用。
	health, err := c.expectStatus(ctx, "健康检查", http.MethodGet, "/health", nil, http.StatusOK)
	if err != nil {
		return err
	}
	if !strings.Contains(string(health), `"ok"`) {
		return fmt.Errorf("健康检查: 响应未声明 ok：%s", snippet(health))
	}
	// state 是各阶段读取的会话状态。
	state, err := c.session(ctx, "初始化前会话")
	if err != nil {
		return err
	}
	if state.Initialized || state.Authenticated {
		return fmt.Errorf("初始化前会话: 全新数据库应为未初始化未登录，实际 %+v", state)
	}
	// err 保存“未登录访问受保护接口”步骤的断言错误，期望服务返回 401。
	if _, err := c.expectStatus(ctx, "未登录访问受保护接口", http.MethodGet, "/api/v1/cards", nil, http.StatusUnauthorized); err != nil {
		return err
	}
	// err 保存首次初始化管理员步骤的断言错误。
	if _, err := c.expectStatus(ctx, "首次初始化管理员", http.MethodPost, "/api/v1/session/initialize", map[string]string{"password": password}, http.StatusOK); err != nil {
		return err
	}
	if state, err = c.session(ctx, "初始化后会话"); err != nil {
		return err
	}
	if !state.Initialized || !state.Authenticated || state.Username != "admin" {
		return fmt.Errorf("初始化后会话: 应自动登录 admin，实际 %+v", state)
	}
	// endpoint 是当前验证的核心列表接口。
	for _, endpoint := range listEndpoints {
		// data 是列表响应体，必须是合法 JSON。
		data, err := c.expectStatus(ctx, "核心列表接口", http.MethodGet, endpoint, nil, http.StatusOK)
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return fmt.Errorf("核心列表接口: %s 返回的不是合法 JSON：%s", endpoint, snippet(data))
		}
	}
	return checkSessionLifecycle(ctx, c, password)
}

// checkSessionLifecycle 验证登出、错误密码被拒、正确密码重新登录以及嵌入式前端页面。
func checkSessionLifecycle(ctx context.Context, c *smokeClient, password string) error {
	// err 保存登出步骤的断言错误。
	if _, err := c.expectStatus(ctx, "登出", http.MethodPost, "/api/v1/session/logout", nil, http.StatusOK); err != nil {
		return err
	}
	// state 是登出后读取的会话状态。
	state, err := c.session(ctx, "登出后会话")
	if err != nil {
		return err
	}
	if state.Authenticated {
		return fmt.Errorf("登出后会话: 仍处于登录态")
	}
	// err 保存错误密码登录步骤的断言错误，期望服务返回 401。
	if _, err := c.expectStatus(ctx, "错误密码登录", http.MethodPost, "/api/v1/session/login", map[string]string{"username": "admin", "password": password + "-wrong"}, http.StatusUnauthorized); err != nil {
		return err
	}
	// err 保存正确密码重新登录步骤的断言错误。
	if _, err := c.expectStatus(ctx, "正确密码登录", http.MethodPost, "/api/v1/session/login", map[string]string{"username": "admin", "password": password}, http.StatusOK); err != nil {
		return err
	}
	// err 保存重新登录后访问受保护接口步骤的断言错误。
	if _, err := c.expectStatus(ctx, "重新登录后访问受保护接口", http.MethodGet, "/api/v1/accounts", nil, http.StatusOK); err != nil {
		return err
	}
	// page 是嵌入式前端首页，必须包含 React 挂载点。
	page, err := c.expectStatus(ctx, "前端首页", http.MethodGet, "/", nil, http.StatusOK)
	if err != nil {
		return err
	}
	if !strings.Contains(string(page), `id="root"`) {
		return fmt.Errorf("前端首页: 缺少 React 挂载点：%s", snippet(page))
	}
	return nil
}
