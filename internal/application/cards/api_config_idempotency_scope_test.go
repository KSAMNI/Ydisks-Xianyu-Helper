package cards

import (
	"strings"
	"testing"
)

// TestAPIRetryIdempotencyScopeMatchesDeliveredPayload 验证幂等重试前置条件只认可真正随请求发送的载荷。
// 保存路径拒绝不会发送幂等键的配置，执行路径仍接受同一配置，避免已保存的历史卡密在升级后停止发卡。
func TestAPIRetryIdempotencyScopeMatchesDeliveredPayload(t *testing.T) {
	// cases 保存按请求方法与幂等键所在位置划分的期望结果。
	cases := []struct {
		// name 是当前用例的可读名称，用于定位失败场景。
		name string
		// raw 是待校验的 API 配置 JSON。
		raw string
		// wantRejected 表示保存路径是否应拒绝该配置。
		wantRejected bool
		// wantMessage 是保存路径拒绝时必须包含的错误文本片段。
		wantMessage string
	}{
		{
			name: "POST 仅正文携带幂等键",
			raw:  `{"url":"https://example.test","method":"POST","retry_enabled":true,"body":{"idempotency_key":"{idempotency_key}"}}`,
		},
		{
			name: "POST 正文为空时参数回退为正文",
			raw:  `{"url":"https://example.test","method":"POST","retry_enabled":true,"params":{"idempotency_key":"{idempotency_key}"}}`,
		},
		{
			name: "GET 查询参数携带幂等键",
			raw:  `{"url":"https://example.test","method":"GET","retry_enabled":true,"params":{"idempotency_key":"{idempotency_key}"}}`,
		},
		{
			name: "请求头对 GET 与 POST 都有效",
			raw:  `{"url":"https://example.test","method":"POST","retry_enabled":true,"headers":{"X-Idempotency-Key":"{idempotency_key}"},"body":{"order_id":"{order_id}"}}`,
		},
		{
			name:         "POST 正文非空时参数不会发送",
			raw:          `{"url":"https://example.test","method":"POST","retry_enabled":true,"params":{"idempotency_key":"{idempotency_key}"},"body":{"order_id":"{order_id}"}}`,
			wantRejected: true,
			wantMessage:  "请求正文",
		},
		{
			name:         "GET 请求正文不会发送",
			raw:          `{"url":"https://example.test","method":"GET","retry_enabled":true,"body":{"idempotency_key":"{idempotency_key}"}}`,
			wantRejected: true,
			wantMessage:  "查询参数",
		},
		{
			name: "未启用重试时不再要求幂等键",
			raw:  `{"url":"https://example.test","method":"POST","body":{"order_id":"{order_id}"}}`,
		},
	}
	for /* testCase 表示当前待校验的方法与占位符位置组合。 */ _, testCase := range cases {
		// _, normalizeErr 保存保存路径对该配置的校验结果。
		_, normalizeErr := normalizeAPIConfig(testCase.raw, "")
		if testCase.wantRejected {
			if normalizeErr == nil || !strings.Contains(normalizeErr.Error(), testCase.wantMessage) {
				t.Fatalf("%s 保存路径应拒绝并提示 %q: err=%v", testCase.name, testCase.wantMessage, normalizeErr)
			}
		} else if normalizeErr != nil {
			t.Fatalf("%s 保存路径不应拒绝: %v", testCase.name, normalizeErr)
		}
		// _, parseErr 保存执行路径对该配置的解析结果；无论保存路径是否拒绝都必须可执行。
		if _, parseErr := ParseAPIConfig(testCase.raw); parseErr != nil {
			t.Fatalf("%s 执行路径必须保持可用: %v", testCase.name, parseErr)
		}
		// summary 保存列表与规则选择使用的脱敏摘要；它必须与保存路径的结论一致。
		summary := SummarizeAPIConfig(testCase.raw)
		if summary.Ready == testCase.wantRejected {
			t.Fatalf("%s 摘要可用性与保存校验不一致: %+v", testCase.name, summary)
		}
		if testCase.wantRejected && !strings.Contains(summary.ValidationError, testCase.wantMessage) {
			t.Fatalf("%s 摘要错误文本应提示 %q: %q", testCase.name, testCase.wantMessage, summary.ValidationError)
		}
	}
}
