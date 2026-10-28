package automation

import (
	"context"

	"xianyu-go/internal/db"
)

// AccountTaskRepository 定义账号任务协调器需要的最小账号与任务持久化能力。
type AccountTaskRepository interface {
	// CheckOrderAutomation 在评价动作前检查指定账号订单是否被用户持久停用，ctx 控制查询取消。
	CheckOrderAutomation(ctx context.Context, accountID, orderID string) error
	// IsPaused 返回账号暂停状态及结束时间。
	IsPaused(ctx context.Context, cookieID string) (bool, int64, error)
	// Status 返回账号是否启用。
	Status(ctx context.Context, cookieID string) (bool, error)
	// GetCookieRuntimeData 返回账号运行所需的 Cookie 与 metadata。
	GetCookieRuntimeData(ctx context.Context, cookieID string) (db.CookieRuntimeData, error)
	// GetValue 返回账号当前 Cookie 明文。
	GetValue(ctx context.Context, cookieID string) (string, error)
	// UpdateValueExisting 更新已存在账号的 Cookie。
	UpdateValueExisting(ctx context.Context, cookieID, cookieValue string) error
	// Get 返回指定账号的任务设置。
	Get(ctx context.Context, cookieID string) (db.AccountTaskSettings, error)
	// Enabled 返回启用中的账号任务设置。
	Enabled(ctx context.Context) ([]db.AccountTaskSettings, error)
	// DueAutoRateOrderIDs 返回由本地买家确认收货事件确认、可消费的订单号。
	DueAutoRateOrderIDs(ctx context.Context, cookieID string, limit int) ([]string, error)
	// MarkOrderSellerRated 记录订单的卖家侧评价事实，使已评价订单不再进入自动评价候选。
	MarkOrderSellerRated(ctx context.Context, cookieID, orderID string) error
	// ClaimRun 抢占可重复执行的任务运行记录。
	ClaimRun(ctx context.Context, run db.AccountTaskRun, now int64) (bool, error)
	// ClaimRunImmediately 抢占人工立即执行的任务运行记录。
	ClaimRunImmediately(ctx context.Context, run db.AccountTaskRun, now int64) (bool, error)
	// FinishRun 写入任务运行结果。
	FinishRun(ctx context.Context, runKey, status string, success, failed int, message string, nextRetryAt int64) error
	// MarkRateScan 记录自动评价扫描时间。
	MarkRateScan(ctx context.Context, cookieID string, at int64) error
	// MarkPolished 记录商品擦亮日期和时间。
	MarkPolished(ctx context.Context, cookieID, date string, at int64) error
}

// accountTaskCredentialLocker 表示账号任务写回凭证时可使用的短临界区锁。
// 锁只保护本地读取与持久化，不得覆盖外部平台调用。
type accountTaskCredentialLocker interface {
	LockAccountCredentials(accountID string) func()
}

// accountTaskRunStateReader 表示能够回读单条运行记录重试状态的仓储能力。
// 它只用于判断自动评价是否已经耗尽自动重试；不具备该能力的旧测试替身按“未耗尽”处理。
type accountTaskRunStateReader interface {
	// RunNextRetryAt 返回运行记录的下一次自动重试时间，0 表示没有后续自动重试。
	RunNextRetryAt(ctx context.Context, runKey string) (int64, error)
}

// storeAccountTaskRepository 将完整 Store 适配为账号任务窄 repository。
type storeAccountTaskRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// LockAccountCredentials 委托账号凭证临界区锁，供任务合并最新 Cookie 快照时避免覆盖并发续期。
func (r storeAccountTaskRepository) LockAccountCredentials(accountID string) func() {
	if r.store == nil {
		return func() {}
	}
	return r.store.LockAccountCredentials(accountID)
}

// storeAccountTaskRepositoryCompileCheck 确保 Store 适配器完整实现账号任务窄接口。
var _ AccountTaskRepository = storeAccountTaskRepository{}

// IsPaused 委托账号暂停状态查询。
func (r storeAccountTaskRepository) IsPaused(ctx context.Context, cookieID string) (bool, int64, error) {
	return r.store.Cookies.IsPaused(ctx, cookieID)
}

// Status 委托账号启用状态查询。
func (r storeAccountTaskRepository) Status(ctx context.Context, cookieID string) (bool, error) {
	return r.store.Cookies.Status(ctx, cookieID)
}

// GetCookieRuntimeData 委托账号运行凭证查询。
func (r storeAccountTaskRepository) GetCookieRuntimeData(ctx context.Context, cookieID string) (db.CookieRuntimeData, error) {
	return r.store.Cookies.GetCookieRuntimeData(ctx, cookieID)
}

// GetValue 委托账号 Cookie 查询。
func (r storeAccountTaskRepository) GetValue(ctx context.Context, cookieID string) (string, error) {
	return r.store.Cookies.GetValue(ctx, cookieID)
}

// UpdateValueExisting 委托账号 Cookie 更新。
func (r storeAccountTaskRepository) UpdateValueExisting(ctx context.Context, cookieID, cookieValue string) error {
	return r.store.Cookies.UpdateValueExisting(ctx, cookieID, cookieValue)
}

// UpdateRenewalCookie 委托保存带完整 metadata 的账号 Cookie。
func (r storeAccountTaskRepository) UpdateRenewalCookie(ctx context.Context, cookieID, cookieValue, metadataJSON string, lastRefreshAt int64) error {
	return r.store.Cookies.UpdateRenewalCookie(ctx, cookieID, cookieValue, metadataJSON, lastRefreshAt)
}

// Get 委托账号任务设置查询。
func (r storeAccountTaskRepository) Get(ctx context.Context, cookieID string) (db.AccountTaskSettings, error) {
	return r.store.AccountTasks.Get(ctx, cookieID)
}

// Enabled 委托启用任务设置查询。
func (r storeAccountTaskRepository) Enabled(ctx context.Context) ([]db.AccountTaskSettings, error) {
	return r.store.AccountTasks.Enabled(ctx)
}

// DueAutoRateOrderIDs 委托读取本地买家确认收货事件确认的自动评价候选订单。
func (r storeAccountTaskRepository) DueAutoRateOrderIDs(ctx context.Context, cookieID string, limit int) ([]string, error) {
	return r.store.AccountTasks.DueAutoRateOrderIDs(ctx, cookieID, limit)
}

// MarkOrderSellerRated 委托写入订单的卖家侧评价事实。
func (r storeAccountTaskRepository) MarkOrderSellerRated(ctx context.Context, cookieID, orderID string) error {
	return r.store.AccountTasks.MarkOrderSellerRated(ctx, cookieID, orderID)
}

// ClaimRun 委托任务运行抢占。
func (r storeAccountTaskRepository) ClaimRun(ctx context.Context, run db.AccountTaskRun, now int64) (bool, error) {
	return r.store.AccountTasks.ClaimRun(ctx, run, now)
}

// ClaimRunImmediately 委托人工任务运行抢占。
func (r storeAccountTaskRepository) ClaimRunImmediately(ctx context.Context, run db.AccountTaskRun, now int64) (bool, error) {
	return r.store.AccountTasks.ClaimRunImmediately(ctx, run, now)
}

// FinishRun 委托任务运行结果写入。
func (r storeAccountTaskRepository) FinishRun(ctx context.Context, runKey, status string, success, failed int, message string, nextRetryAt int64) error {
	return r.store.AccountTasks.FinishRun(ctx, runKey, status, success, failed, message, nextRetryAt)
}

// RunNextRetryAt 委托运行记录下一次自动重试时间查询，供自动评价终态通知判断重试是否耗尽。
func (r storeAccountTaskRepository) RunNextRetryAt(ctx context.Context, runKey string) (int64, error) {
	return r.store.AccountTasks.RunNextRetryAt(ctx, runKey)
}

// storeAccountTaskRunStateCheck 确保 Store 适配器实现运行重试状态回读能力。
var _ accountTaskRunStateReader = storeAccountTaskRepository{}

// MarkRateScan 委托自动评价扫描时间写入。
func (r storeAccountTaskRepository) MarkRateScan(ctx context.Context, cookieID string, at int64) error {
	return r.store.AccountTasks.MarkRateScan(ctx, cookieID, at)
}

// MarkPolished 委托商品擦亮时间写入。
func (r storeAccountTaskRepository) MarkPolished(ctx context.Context, cookieID, date string, at int64) error {
	return r.store.AccountTasks.MarkPolished(ctx, cookieID, date, at)
}

// newStoreAccountTaskRepository 从完整 Store 构造账号任务窄 repository。
func newStoreAccountTaskRepository(store *db.Store) AccountTaskRepository {
	if store == nil || store.Cookies == nil || store.AccountTasks == nil {
		return nil
	}
	return storeAccountTaskRepository{store: store}
}
