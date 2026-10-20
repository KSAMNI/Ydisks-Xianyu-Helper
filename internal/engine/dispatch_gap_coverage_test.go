package engine

import (
	"testing"
)

// TestExtractMessageReadEventNestedBatch 验证兼容信封嵌套在数组中时命中首个 40103 事件并忽略后续元素。
func TestExtractMessageReadEventNestedBatch(t *testing.T) {
	// payload 是外层对象包裹的批量事件：第一个元素是回执，第二个元素应在命中后被忽略。
	payload := map[string]any{"events": []any{
		map[string]any{"bizType": "40103", "messageId": "9.PNM", "cid": "chat-1"},
		map[string]any{"other": map[string]any{"bizType": "40103", "messageId": "ignored"}},
	}}
	// event、ok 是提取出的已读事件及命中状态。
	event, ok := extractMessageReadEvent(payload)
	if !ok || event.MessageID != "9.PNM" || event.ChatID != "chat-1" {
		t.Fatalf("批量回执解析异常: %+v ok=%v", event, ok)
	}
}

// TestExtractMessageIDFallbackOrder 验证消息 ID 在缺字段、PNM 数组、bizTag、extJson 下的兜底顺序。
func TestExtractMessageIDFallbackOrder(t *testing.T) {
	if extractMessageID(map[string]any{}) != "" {
		t.Fatal("缺少顶层信封应返回空")
	}
	// pnmInArray 的 PNM 标识只出现在数组里。
	pnmInArray := map[string]any{"1": map[string]any{"list": []any{"77.PNM"}}}
	// got 保存被测函数的实际输出，随后与期望值比较
	if got := extractMessageID(pnmInArray); got != "77.PNM" {
		t.Fatalf("数组内 PNM 解析异常: %q", got)
	}
	// bizTag、extJson 分别携带兼容关联 ID。
	bizTag := map[string]any{"1": map[string]any{"10": map[string]any{"bizTag": `{"messageId":"biz-1"}`}}}
	// got 保存被测函数的实际输出，随后与期望值比较
	if got := extractMessageID(bizTag); got != "biz-1" {
		t.Fatalf("bizTag 解析异常: %q", got)
	}
	// extJSON 是携带消息扩展字段的测试数据
	extJSON := map[string]any{"1": map[string]any{"10": map[string]any{"extJson": `{"messageId":"ext-1"}`}}}
	// got 保存被测函数的实际输出，随后与期望值比较
	if got := extractMessageID(extJSON); got != "ext-1" {
		t.Fatalf("extJson 解析异常: %q", got)
	}
}

// TestExtractChatMessageAndEchoStructureGuards 验证聊天与自身回显提取在缺少信封、展示扩展或会话 ID 时返回空。
func TestExtractChatMessageAndEchoStructureGuards(t *testing.T) {
	// noEnvelope 缺少顶层聊天信封。
	noEnvelope := map[string]any{}
	// noExtension 缺少展示扩展。
	noExtension := map[string]any{"1": map[string]any{"2": "chat@goofish"}}
	if extractChatMessage(noEnvelope, "a", "unb=1") != nil || extractChatMessage(noExtension, "a", "unb=1") != nil {
		t.Fatal("缺少信封或扩展的聊天消息应忽略")
	}
	if extractOwnWebSocketEcho(noEnvelope, "a", "unb=1") != nil || extractOwnWebSocketEcho(noExtension, "a", "unb=1") != nil {
		t.Fatal("缺少信封或扩展的回显应忽略")
	}
	// emptyChat 是发送者为自身但会话 ID 为空的回显。
	emptyChat := map[string]any{"1": map[string]any{"2": "", "10": map[string]any{"reminderContent": "hi", "senderUserId": "1"}}}
	if extractOwnWebSocketEcho(emptyChat, "a", "unb=1") != nil {
		t.Fatal("空会话 ID 的回显应忽略")
	}
}

// TestExtractOwnEchoItemCardAndWalkers 验证商品卡片回显、图片与商品卡片递归提取的各种嵌套形态。
func TestExtractOwnEchoItemCardAndWalkers(t *testing.T) {
	// card 是商品卡片对象，主图使用协议相对地址。
	card := map[string]any{"itemCard": map[string]any{"item": map[string]any{"itemId": "i1", "title": "标题", "mainPic": "//img.example/a.jpg", "price": "¥9"}}}
	// echo 是 contentType=7 的自身回显帧，卡片位于 m1["6"]。
	echo := map[string]any{"1": map[string]any{
		"2": "chat-1@goofish", "6": card,
		"10": map[string]any{"reminderContent": "[商品卡片]", "senderUserId": "1", "extJson": `{"contentType":"7"}`},
	}}
	// message 是提取出的自身回显观察。
	message := extractOwnWebSocketEcho(echo, "acc", "unb=1")
	if message == nil || message.MessageType != "item" || message.Content == "" {
		t.Fatalf("商品卡片回显解析异常: %+v", message)
	}
	// array 是数组顶层的卡片列表：命中第一个后，第二个元素应被忽略。
	if got := extractItemCardObservationContent([]any{card, card}); got == "" {
		t.Fatal("数组中的商品卡片应被提取")
	}
	// nested 把卡片放在无效卡片对象的子字段里，验证对象递归。
	nested := map[string]any{"itemCard": map[string]any{"item": map[string]any{"itemId": "only-id"}}, "child": card}
	// got 保存被测函数的实际输出，随后与期望值比较
	if got := extractItemCardObservationContent(nested); got == "" {
		t.Fatal("对象子字段中的卡片应被提取")
	}
	// imageFrame 的首个图片地址不是 http，随后在兄弟数组里出现可用图片，并带有空节点。
	imageFrame := map[string]any{
		"image": map[string]any{"pics": []any{map[string]any{"url": "ftp://x/a.jpg"}}},
		"nil":   nil,
		"list":  []any{map[string]any{"image": map[string]any{"pics": []any{map[string]any{"url": "https://img.example/1.png"}}}}},
	}
	// got 保存被测函数的实际输出，随后与期望值比较
	if got := extractImageObservationContent(imageFrame); got != "https://img.example/1.png" {
		t.Fatalf("图片递归提取异常: %q", got)
	}
	// got 保存被测函数的实际输出，随后与期望值比较
	if got := extractImageObservationContent(`{"image":{"pics":[{"url":"https://img.example/2.png"}]}}`); got != "https://img.example/2.png" {
		t.Fatalf("字符串封装图片提取异常: %q", got)
	}
}

// TestMessageContentTypeFallbacks 验证消息类型在 extJson 缺失、m6 缺失、内容 JSON 无效时的兜底。
func TestMessageContentTypeFallbacks(t *testing.T) {
	// cases 描述各层兜底输入与期望的类型。
	cases := []struct {
		// name 是用例名称。
		name string
		// m1、m10 是消息信封与展示扩展。
		m1, m10 map[string]any
		// want 是期望的内容类型。
		want string
	}{
		{name: "extJson无类型且无m6", m1: map[string]any{}, m10: map[string]any{"extJson": `{"x":1}`}, want: ""},
		{name: "m6无3", m1: map[string]any{"6": map[string]any{}}, m10: map[string]any{}, want: ""},
		{name: "m63类型", m1: map[string]any{"6": map[string]any{"3": map[string]any{"4": "3"}}}, m10: map[string]any{}, want: "3"},
		{name: "m63内容JSON", m1: map[string]any{"6": map[string]any{"3": map[string]any{"5": `{"contentType":"2"}`}}}, m10: map[string]any{}, want: "2"},
		{name: "m63内容无效", m1: map[string]any{"6": map[string]any{"3": map[string]any{"5": "not-json"}}}, m10: map[string]any{}, want: ""},
	}
	// tc 是当前兜底用例。
	for _, tc := range cases {
		// got 保存被测函数的实际输出，随后与期望值比较
		if got := messageContentType(tc.m1, tc.m10); got != tc.want {
			t.Fatalf("%s: got=%q want=%q", tc.name, got, tc.want)
		}
	}
}
