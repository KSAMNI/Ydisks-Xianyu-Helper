package automation

import (
	"testing"
	"time"
)

// TestReleasePendingShipAttemptClearsCooldown 覆盖未取得运行权时释放冷却预约，使同一订单可立即再次扫描。
func TestReleasePendingShipAttemptClearsCooldown(t *testing.T) {
	// scheduler 是直接构造的调度器；冷却表带有一个预约。
	scheduler := &Scheduler{pendingShipCooldown: map[string]time.Time{"order-1": time.Now(), "order-2": time.Now()}}
	scheduler.releasePendingShipAttempt("order-1")
	// kept 保存该步骤的中间结果，供后续断言使用。
	if _, kept := scheduler.pendingShipCooldown["order-1"]; kept {
		t.Fatal("order-1 的冷却预约应被释放")
	}
	// kept 保存该步骤的中间结果，供后续断言使用。
	if _, kept := scheduler.pendingShipCooldown["order-2"]; !kept {
		t.Fatal("其他订单的冷却预约不应受影响")
	}
	(*Scheduler)(nil).releasePendingShipAttempt("order-1")
}

// TestClearTemplateMessageValuesRemovesOnlyMessageKeys 覆盖渲染为空的消息清理其变量缓存，以便下一条消息重新取卡。
func TestClearTemplateMessageValuesRemovesOnlyMessageKeys(t *testing.T) {
	// state 带有两个已加载变量。
	state := &templateDeliveryState{values: map[string]string{"a": "1", "b": "2"}, loadedKeys: map[string]bool{"a": true, "b": true}}
	(&automationActionExecutor{}).clearTemplateMessageValues(state, "{{cards.a}}")
	// kept 保存该步骤的中间结果，供后续断言使用。
	if _, kept := state.values["a"]; kept || state.loadedKeys["a"] {
		t.Fatal("消息使用的变量应被清理")
	}
	if state.values["b"] != "2" || !state.loadedKeys["b"] {
		t.Fatal("未使用的变量应保留")
	}
}
