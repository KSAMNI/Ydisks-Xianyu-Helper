package adapter

import "testing"

// TestDependencyFactoriesReturnNilForNilReceivers 验证各领域依赖工厂在接收者为空时返回空结果而不是 panic。
func TestDependencyFactoriesReturnNilForNilReceivers(t *testing.T) {
	// items、automation、system、orders、misc 是五类空指针依赖容器。
	var items *ItemDependencies
	// automationDeps 测试中的局部对象，承载被测流程的输入、依赖或输出。
	var automationDeps *AutomationDependencies
	// system 测试中的局部对象，承载被测流程的输入、依赖或输出。
	var system *SystemDependencies
	// orders 测试中的局部对象，承载被测流程的输入、依赖或输出。
	var orders *OrderDependencies
	// misc 测试中的局部对象，承载被测流程的输入、依赖或输出。
	var misc *MiscDependencies
	// checks 按名称记录每个工厂是否返回了空结果。
	checks := map[string]bool{
		"item batch repository":   items.NewItemBatchRepository() == nil,
		"item batch preview":      items.NewItemBatchPreviewPort() == nil,
		"item batch publish":      items.NewItemBatchPublishPort(nil, nil, nil, nil, nil, nil) == nil,
		"item publish port":       items.NewItemPublishPort(nil, nil, nil, nil) == nil,
		"item publish repository": items.NewItemPublishRepository() == nil,
		"item catalog":            items.NewItemCatalogRepository() == nil,
		"item ai settings":        items.NewItemAISettingsRepository() == nil,
		"item sync":               items.NewItemSyncRepository(nil, nil, nil, nil) == nil,
		"credential wake":         automationDeps.NewAutomationCredentialWakeRepository() == nil,
		"automation":              automationDeps.NewAutomationRepository() == nil,
		"delivery template":       automationDeps.NewDeliveryTemplateRepository() == nil,
		"account task":            automationDeps.NewAccountTaskRepository() == nil,
		"default reply":           automationDeps.NewDefaultReplyRepository() == nil,
		"keyword":                 automationDeps.NewKeywordRepository() == nil,
		"database health":         system.NewDatabaseHealth() == nil,
		"reconciliation service":  system.NewReconciliationService(nil) == nil,
		"order repository":        orders.NewOrderRepository() == nil,
		"order reconciliation":    orders.NewOrderReconciliationRepository() == nil,
		"order runtime":           orders.NewOrderRuntime(OrderRuntimeHooks{}, nil, nil) == nil,
		"order refresh job":       orders.NewOrderRefreshJobRepository() == nil,
		"notification uncertain":  misc.NewNotificationUncertainRepository() == nil,
		"notification channel":    misc.NewNotificationChannelRepository() == nil,
		"analytics":               misc.NewAnalyticsRepository() == nil,
		"cards":                   misc.NewCardsRepository() == nil,
		"api card tester":         misc.NewAPICardTester(nil) == nil,
		"nil system constructor":  NewSystemDependencies(nil) == nil,
	}
	// name、isNil 是检查名称与是否返回空。
	for name, isNil := range checks {
		if !isNil {
			t.Errorf("%s 在空接收者上应返回空结果", name)
		}
	}
}
