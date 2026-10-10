package notify

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestManualTaskLabelsAndOutboxDetails 验证每种通知能明确识别求评价、发货及免拼，旧类别订阅和稳定去重仍生效。
func TestManualTaskLabelsAndOutboxDetails(t *testing.T) {
	// labels 覆盖全部已知类型以及缺省和未来类型；订单创建与付款事件尚无规则运行，使用事件阶段名而非业务任务名。
	labels := map[string]string{"order_paid": "付款发货事件（卖家身份待核验）", "order_created": "订单创建事件（卖家身份待核验）", "buyer_reviewed": "评价赠品", "review_missing_timeout": "求评价", "bargain_pending": "砍价自动免拼", "order_completed": "确认收货", "": "未知自动化任务", "future": "其他任务（future）"}
	// store、cleanup 是本地通知 outbox，不实际发送到外部渠道。
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	addWebhookChannel(t, store, "cid", "人工处理", "http://127.0.0.1:1")
	// notifier 使用真实通知格式和 outbox 幂等逻辑。
	notifier := New("cid", store, nil)
	for trigger, label := range labels { // trigger、label 是原始业务事件及应展示的中文名称。
		if got := manualTaskLabel(trigger); got != label {
			t.Fatalf("label=%s want=%s", got, label)
		} // got 验证任务名称不会统一误标为发货。
		// key 按场景生成稳定业务键，重复调用应只有一条持久化通知。
		key := fmt.Sprintf("manual-label:%s", trigger)
		notifier.NotifyManualIntervention(context.Background(), trigger, "cid", "order-detail", "item", "buyer", "延期重试耗尽", "需要检查", "chat", key)
		notifier.NotifyManualIntervention(context.Background(), trigger, "cid", "order-detail", "item", "buyer", "延期重试耗尽", "需要检查", "chat", key)
		// body、count 保存实际通知正文与去重数量，不读取渠道凭证。
		var body string
		// count 验证相同事件不会因为信息增强而重复入队。
		var count int
		// err 确认通知已实际写入 outbox，查询失败不能视作去重成功。
		if err := store.DB.QueryRowContext(context.Background(), `SELECT MIN(body),COUNT(*) FROM notification_outbox WHERE idempotency_key=?`, key).Scan(&body, &count); err != nil {
			t.Fatal(err)
		} // err 保证实际 outbox 已落库。
		if count != 1 || !strings.Contains(body, label) || !strings.Contains(body, "order-detail") || !strings.Contains(body, "延期重试耗尽") {
			t.Fatalf("通知缺少任务类型/订单/环节或重复入队: count=%d", count)
		}
	}
}
