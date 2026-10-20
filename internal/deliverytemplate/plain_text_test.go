package deliverytemplate

import "testing"

// TestParseAcceptsPlainTextWithoutVariables 验证不含任何变量标记的消息被原样接受且不产生变量键。
func TestParseAcceptsPlainTextWithoutVariables(t *testing.T) {
	// parsed 和 err 是纯文本模板的解析结果。
	parsed, err := Parse([]string{"谢谢惠顾"})
	if err != nil || len(parsed.Keys) != 0 || len(parsed.Messages) != 1 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
}
