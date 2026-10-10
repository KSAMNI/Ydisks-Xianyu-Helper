import type { AutomationRunIssue,DeferredAutomationIssue } from './api';

// AutomationResolution 表示自动化运行异常可执行的人工处理动作。
export type AutomationResolution = 'continue' | 'retry' | 'cancel' | 'stop_order';

/** DeferredResolution 保留单任务忽略并区分持久整单停用。 */
export type DeferredResolution = 'retry' | 'dismiss' | 'stop_order';

// AutomationIssueState 保存规则页异常面板所需的两类异常记录。
export interface AutomationIssueState {
  // runs 是需要人工确认的自动化运行记录。
  runs: AutomationRunIssue[];
  // pending_tasks 是达到重试上限的延迟任务记录。
  pending_tasks: DeferredAutomationIssue[];
}

// canResolveAutomationIssue 判断后端策略是否允许指定处理动作。
export const canResolveAutomationIssue = (
  issue: AutomationRunIssue,
  resolution: AutomationResolution,
): boolean => resolution === 'stop_order' ? Boolean(issue.order_id && issue.can_stop_order) : issue.allowed_resolutions.includes(resolution);

// filterAutomationIssues 按账号筛选异常，保持异常面板与规则列表一致。
export const filterAutomationIssues = (issues: AutomationIssueState, cookieID: string): AutomationIssueState => ({
  runs: issues.runs.filter(
    // 运行异常筛选器只保留当前账号的记录。
    issue => !cookieID || issue.cookie_id === cookieID,
  ),
  pending_tasks: issues.pending_tasks.filter(
    // 延迟任务筛选器只保留当前账号的记录。
    issue => !cookieID || issue.cookie_id === cookieID,
  ),
});

// automationIssueKindLabel 将后端异常类别转换成用户可读的中文标签。
export const automationIssueKindLabel = (kind: AutomationRunIssue['issue_kind']): string => ({
  external_result_unknown: '外部动作结果未知',
  invalid_snapshot: '历史数据无法恢复',
  rule_unavailable: '规则不可用',
  partial_failure: '部分动作失败',
  execution_failed: '动作执行失败',
}[kind] || '自动化异常');

// AutomationPageDataLoaders 描述规则页规则列表与异常列表的独立加载器。
export interface AutomationPageDataLoaders<TRule> {
  // loadRules 加载自动化规则列表。
  loadRules: () => Promise<TRule[]>;
  // loadIssues 加载自动化异常列表。
  loadIssues: () => Promise<AutomationIssueState>;
  // onRules 接收规则列表成功结果。
  onRules: (rules: TRule[]) => void;
  // onIssues 接收异常列表成功结果。
  onIssues: (issues: AutomationIssueState) => void;
  // onIssuesError 接收异常列表失败结果。
  onIssuesError: (error: unknown) => void;
}

// loadAutomationPageData 并行加载规则与异常，且允许异常接口单独失败。
export const loadAutomationPageData = async <TRule>(options: AutomationPageDataLoaders<TRule>): Promise<void> => {
  // issuesPromise 先启动异常请求，避免阻塞主要规则列表。
  const issuesPromise = options.loadIssues().then(options.onIssues).catch(options.onIssuesError);
  // rules 是主要规则请求的成功结果。
  const rules = await options.loadRules();
  options.onRules(rules);
  await issuesPromise;
};

/** automationTaskLabel 将触发类型转换成明确业务名称；未知类型保留原值便于定位，不能笼统标作发货。 */
export const automationTaskLabel = (trigger: string): string => ({
  order_paid: '付款发货', order_created: '拍下改价', buyer_reviewed: '评价赠品',
  review_missing_timeout: '求评价', bargain_pending: '砍价自动免拼', order_completed: '确认收货', auto_rate: '自动评价买家',
}[trigger] || (trigger ? `其他任务（${trigger}）` : '未知任务'));

/** deferredTaskLabel 用于尚未匹配到规则运行的延期事件；此时不能断言已命中“拍下改价/付款发货”规则，
 *  只能描述为待核验的事件阶段，避免误导用户认为规则已执行。 */
export const deferredTaskLabel = (trigger: string): string => ({
  order_paid: '付款发货事件（卖家身份待核验）', order_created: '订单创建事件（卖家身份待核验）',
}[trigger] || automationTaskLabel(trigger));

/** automationOrderStatusLabel 显示本地订单阶段；缺失及未来状态不猜测为待发货。 */
export const automationOrderStatusLabel = (status?: string): string => ({
 pending_pay: '待付款', pending_ship: '待发货', pending_receive: '待收货', shipped: '已发货', completed: '已完成', canceled: '已取消', cancelled: '已取消', closed: '已关闭',
}[status || ''] || status || '尚未获取');

/** issueResolutionPrompt 根据明确的处理范围生成二次确认；issue 可选以兼容既有单任务调用。 */
export const issueResolutionPrompt = (resolution: AutomationResolution | DeferredResolution, issue?: AutomationRunIssue | DeferredAutomationIssue): string => {
  /** context 只包含任务类型、账号与订单身份，不含消息内容和凭证；延期死信尚无规则运行，使用事件阶段名。 */
  const isDeferred = issue != null && 'attempt_count' in issue;
  const taskLabel = issue == null ? '' : (isDeferred ? deferredTaskLabel(issue.trigger_type) : automationTaskLabel(issue.trigger_type));
  const context = issue ? `任务：${taskLabel}\n账号：${issue.account_name || issue.cookie_id}\n订单：${issue.order_id || '尚未关联订单'}\n\n` : '';
  if (resolution === 'stop_order') return context + '确认停止此订单全部自动化？\n包括发货、确认发货、免拼、评价赠品、求评价及自动评价买家。重启后仍有效，不影响其他订单。\n已发送内容或正在执行的平台请求不能撤回。不会删除执行记录，也不会自动恢复历史任务。';
  if (resolution === 'continue') return context + '确认外部动作已经执行成功，并跳到下一步吗？';
  if (resolution === 'retry') return context + '确认外部动作没有执行，可以安全重试吗？错误判断可能造成重复发送。';
  return context + (resolution === 'cancel' ? '确认终止本次任务？这不会停止该订单其他自动任务。' : '确认彻底删除这条出错的延期任务？\\n删除后不可恢复，该记录将从列表中移除且不再重试、不再告警。\\n这不会停止该订单的后续自动任务；若平台再次推送同类事件且仍无法核验，会新建一条记录。');
};
