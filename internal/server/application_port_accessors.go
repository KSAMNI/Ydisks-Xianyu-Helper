package server

// 本文件只承载 Server 读取组合期注入的应用端口访问器；端口定义与校验仍留在 application_ports.go，
// 使 HTTP 传输层保持“只取用已装配端口”的单一职责。

// accountLoginApplication 返回 Cookie 与二维码登录用例 Port。
func (server *Server) accountLoginApplication() AccountLoginPort {
	return server.applicationServiceSet().accountLogin
}

// qrLoginApplication 返回二维码平台流程用例 Port。
func (server *Server) qrLoginApplication() QRLoginPort {
	return server.applicationServiceSet().qrLogin
}

// accountRuntimeApplication 返回账号运行时状态用例。
func (server *Server) accountRuntimeApplication() AccountRuntimePort {
	return server.applicationServiceSet().accountRuntime
}

// orderRefreshJobsApplication 返回订单刷新任务用例。
func (server *Server) orderRefreshJobsApplication() OrderRefreshJobsPort {
	return server.applicationServiceSet().orderRefreshJobs
}

// itemCatalogApplication 返回商品目录读取用例。
func (server *Server) itemCatalogApplication() ItemCatalogPort {
	return server.applicationServiceSet().itemCatalog
}

// itemSinglePublishApplication 返回单商品发布用例。
func (server *Server) itemSinglePublishApplication() ItemSinglePublishPort {
	return server.applicationServiceSet().itemSinglePublish
}

// itemCatalogMutationApplication 返回商品目录写入用例。
func (server *Server) itemCatalogMutationApplication() ItemCatalogMutationPort {
	return server.applicationServiceSet().itemCatalogMutation
}

// itemAISettingsApplication 返回商品级 AI 配置用例。
func (server *Server) itemAISettingsApplication() ItemAISettingsPort {
	return server.applicationServiceSet().itemAISettings
}

// itemSyncApplication 返回商品同步用例。
func (server *Server) itemSyncApplication() ItemSyncPort {
	return server.applicationServiceSet().itemSync
}

// libraryApplication 返回本地资源库用例。
func (server *Server) libraryApplication() LibraryPort {
	return server.applicationServiceSet().library
}

// cardsApplication 返回卡券库存用例。
func (server *Server) cardsApplication() CardsPort {
	return server.applicationServiceSet().cards
}

// imageLibraryApplication 返回资源中心图片用例。
func (server *Server) imageLibraryApplication() ImageLibraryPort {
	return server.applicationServiceSet().imageLibrary
}

// accountTaskApplication 返回账号自动化任务用例。
func (server *Server) accountTaskApplication() AccountTasksPort {
	return server.applicationServiceSet().accountTasks
}

// automationIssuesApplication 返回自动化异常用例。
func (server *Server) automationIssuesApplication() AutomationIssuesPort {
	return server.applicationServiceSet().automationIssues
}

// automationAuditApplication 返回规则日志查询用例。
func (server *Server) automationAuditApplication() AutomationAuditPort {
	return server.applicationServiceSet().automationAudit
}

// automationRulesApplication 返回自动化规则用例。
func (server *Server) automationRulesApplication() AutomationRulesPort {
	return server.applicationServiceSet().automationRules
}

// deliveryTemplatesApplication 返回发货模板管理用例。
func (server *Server) deliveryTemplatesApplication() DeliveryTemplatesPort {
	return server.applicationServiceSet().deliveryTemplates
}

// itemBatchPreviewApplication 返回批量发布预检用例。
func (server *Server) itemBatchPreviewApplication() ItemBatchPreviewPort {
	return server.applicationServiceSet().itemBatchPreview
}

// itemBatchManagementApplication 返回批量发布管理用例。
func (server *Server) itemBatchManagementApplication() ItemBatchManagementPort {
	return server.applicationServiceSet().itemBatchManagement
}

// itemCategoryRecommendationApplication 返回类目推荐用例。
func (server *Server) itemCategoryRecommendationApplication() ItemCategoryRecommendationPort {
	return server.applicationServiceSet().itemCategoryRecommendation
}

// itemBatchPreviewPersistenceApplication 返回批量预检保存用例。
func (server *Server) itemBatchPreviewPersistenceApplication() ItemBatchPreviewPersistencePort {
	return server.applicationServiceSet().itemBatchPreviewPersistence
}

// accountProfileApplication 返回账号资料刷新用例。
func (server *Server) accountProfileApplication() AccountProfilePort {
	return server.applicationServiceSet().accountProfile
}

// accountLongLoginApplication 返回账号长登录用例。
func (server *Server) accountLongLoginApplication() AccountLongLoginPort {
	return server.applicationServiceSet().accountLongLogin
}

// accountSettingsApplication 返回账号设置用例。
func (server *Server) accountSettingsApplication() AccountSettingsPort {
	return server.applicationServiceSet().accountSettings
}

// platformCredentialApplication 返回受控的平台凭证读取用例。
func (server *Server) platformCredentialApplication() PlatformCredentialPort {
	return server.applicationServiceSet().platformCredentials
}

// authenticationApplication 返回用户登录认证用例。
func (server *Server) authenticationApplication() AuthenticationPort {
	return server.applicationServiceSet().authentication
}

// accountSummaryApplication 返回账号摘要用例。
func (server *Server) accountSummaryApplication() AccountSummaryPort {
	return server.applicationServiceSet().accountSummaries
}

// accountDeleteApplication 返回账号删除用例。
func (server *Server) accountDeleteApplication() AccountDeletePort {
	return server.applicationServiceSet().accountDelete
}

// loginAuditApplication 返回登录审计用例。
func (server *Server) loginAuditApplication() LoginAuditPort {
	return server.applicationServiceSet().loginAudit
}

// settingsApplication 返回系统设置用例。
func (server *Server) settingsApplication() SettingsPort {
	return server.applicationServiceSet().settings
}

// applicationServiceSet 返回构造期注入的不可变 Port 快照；零值 Server 不会隐式装配业务服务。
func (server *Server) applicationServiceSet() *ApplicationPorts {
	if server == nil || server.applications == nil {
		return emptyApplicationPorts
	}
	return server.applications
}
