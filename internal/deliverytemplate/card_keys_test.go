package deliverytemplate

import (
	"reflect"
	"testing"
)

// TestCardKeysExtractsOrderedUniqueKeys 覆盖单条消息中卡密变量键的按序去重提取，以及无变量/自定义变量不被误提取。
func TestCardKeysExtractsOrderedUniqueKeys(t *testing.T) {
	// cases 保存消息文本到期望变量键的映射。
	cases := []struct {
		// message 是待解析的模板消息。
		message string
		// want 是期望的卡密变量键（按首次出现顺序）。
		want []string
	}{
		{"{{cards.a}} 和 {{cards.b}} 与 {{cards.a}}", []string{"a", "b"}},
		{"{{delivery.cards.first}}", []string{"first"}},
		{"只有 {{custom.note}} 与 {{order_id}}", []string{}},
		{"", []string{}},
	}
	// tc 是当前待验证的消息用例。
	for _, tc := range cases {
		// got 保存本次提取到的变量键，用于与期望顺序比较。
		if got := CardKeys(tc.message); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q got %v want %v", tc.message, got, tc.want)
		}
	}
}
