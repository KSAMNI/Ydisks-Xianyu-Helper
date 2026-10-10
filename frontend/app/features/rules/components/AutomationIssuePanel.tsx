import { AlertCircle } from 'lucide-react';
import type { AutomationRunIssue, DeferredAutomationIssue } from '../api';
import { automationIssueKindLabel, automationTaskLabel, deferredTaskLabel, automationOrderStatusLabel, canResolveAutomationIssue, type AutomationResolution, type DeferredResolution } from '../issueState';
import { copyAutomationOrderID } from '../issueActions';

/** AutomationIssuePanelProps 保持运行与延期两类异常及其服务端处理能力独立。 */
export interface AutomationIssuePanelProps {
  /** runs 是需要核对动作结果的运行记录。 */ runs: AutomationRunIssue[];
  /** pendingTasks 是达到重试上限的延期记录。 */ pendingTasks: DeferredAutomationIssue[];
  /** busy 表示本页正在提交人工决策，期间禁止重复操作。 */ busy?: boolean;
  /** onResolveRun 交由父级处理运行，issue 仅用于确认文案，服务端独立验证归属。 */
  onResolveRun: (id: number, resolution: AutomationResolution, issue?: AutomationRunIssue) => void;
  /** onResolveDeferredTask 交由父级处理延期任务或整单停用。 */
  onResolveDeferredTask: (id: number, resolution: DeferredResolution, issue?: DeferredAutomationIssue) => void;
}

/** IssueIdentity 展示可确认的订单上下文；issue 不含原始快照、卡密或凭证，缺字段明确提示而非猜测。 */
const IssueIdentity = ({ issue }: { /** issue 是待展示的非敏感运行或延期摘要。 */ issue: AutomationRunIssue | DeferredAutomationIssue }) => (
  <div className="text-sm text-gray-700 space-y-1 break-words">
    <div>账号：{issue.account_name || issue.cookie_id}{issue.account_name && issue.account_name !== issue.cookie_id ? `（${issue.cookie_id}）` : ''}</div>
    <div>订单：<span className="font-mono select-all">{issue.order_id || '尚未关联订单'}</span>
      {issue.order_id && <span className="ml-3 inline-flex gap-3">
        <button type="button" className="text-brand font-bold" onClick={/* 当前回调只复制所展示的订单标识。 */ () => void copyAutomationOrderID(issue.order_id!)}>复制订单号</button>
        <a className="text-brand font-bold" href={`https://www.goofish.com/order-detail?orderId=${encodeURIComponent(issue.order_id)}&role=seller`} target="_blank" rel="noopener noreferrer">查看闲鱼订单</a>
      </span>}
    </div>
    <div>商品：{issue.item_title || '尚未获取'}{issue.item_id ? `（${issue.item_id}）` : ''}</div>
    <div>买家：{issue.buyer_id || '尚未获取'} · 订单状态：{automationOrderStatusLabel(issue.order_status)}</div>
    {issue.chat_id && <div>会话：{issue.chat_id}</div>}
    <div className="text-xs text-gray-500">最近更新：{issue.updated_at || '未知'}</div>
    {!issue.can_stop_order && <div className="text-xs text-amber-700">未确认订单身份或当前归属，只能处理本次任务，不能停止整单。</div>}
  </div>
);

/** AutomationIssuePanel 明确展示求评价、发货等任务类型；操作能力仍来自后端，不因展示更多按钮而放宽安全重放。 */
export const AutomationIssuePanel = ({ runs, pendingTasks, busy = false, onResolveRun, onResolveDeferredTask }: AutomationIssuePanelProps) => (
  <section className="rounded-2xl border border-red-200 bg-red-50 p-5 space-y-4" aria-busy={busy}>
    <div className="flex items-start gap-3">
      <AlertCircle className="w-5 h-5 text-red-600 mt-0.5" />
      <div><h3 className="font-black text-red-900">需要人工处理的自动化任务</h3>
        <p className="text-sm text-red-700 mt-1">请先核对闲鱼订单和聊天记录。终止本次任务不等于停止整单；停止整单后，后续自动化及历史重试均不会再执行。</p>
        <p className="text-xs text-red-700 mt-1">延期任务的「彻底删除」会永久移除该条出错记录（不可恢复），不再重试也不再告警；已确认完成的动作数不等于卡密张数；未记录成功也不能证明平台未执行。已发送内容或正在执行的请求无法撤回。</p>
      </div>
    </div>
    {runs.map(/* issue 是当前需要人工核对的运行，只展示其允许的处理动作。 */ issue => (
      <article key={`run-${issue.id}`} className="rounded-xl border border-red-100 bg-white p-4 space-y-3">
        <div className="font-bold text-gray-900">任务：{automationTaskLabel(issue.trigger_type)} <span className="text-xs text-gray-500">· 运行 #{issue.id}</span></div>
        <IssueIdentity issue={issue} />
        <div className="text-sm">执行进度：已确认完成 {issue.sent_count} 个动作 · 当前检查点第 {issue.action_cursor + 1} 步</div>
        <div className="text-xs font-bold text-red-800">{automationIssueKindLabel(issue.issue_kind)}</div>
        <div className="text-sm text-red-700 whitespace-pre-wrap break-words">停止原因：{issue.error_message || '未提供具体原因，请核对实际结果'}</div>
        <div className="flex flex-wrap gap-2">
          {canResolveAutomationIssue(issue, 'continue') && <button disabled={busy} onClick={/* 当前回调要求用户核对已执行结果后继续下一步。 */ () => onResolveRun(issue.id, 'continue', issue)} className="px-3 py-2 rounded-lg bg-emerald-100 text-emerald-800 text-xs font-bold disabled:opacity-50">已执行，继续下一步</button>}
          {canResolveAutomationIssue(issue, 'retry') && <button disabled={busy} onClick={/* 当前回调只在后端允许安全重试时提交。 */ () => onResolveRun(issue.id, 'retry', issue)} className="px-3 py-2 rounded-lg bg-amber-100 text-amber-800 text-xs font-bold disabled:opacity-50">未执行，安全重试</button>}
          {canResolveAutomationIssue(issue, 'cancel') && <button disabled={busy} onClick={/* 当前回调只终止这个运行，不停用整单。 */ () => onResolveRun(issue.id, 'cancel', issue)} className="px-3 py-2 rounded-lg bg-gray-100 text-gray-700 text-xs font-bold disabled:opacity-50">终止本次任务</button>}
          {canResolveAutomationIssue(issue, 'stop_order') && <button disabled={busy} onClick={/* 当前回调经独立二次确认后持久停用订单全部自动化。 */ () => onResolveRun(issue.id, 'stop_order', issue)} className="px-3 py-2 rounded-lg bg-red-100 text-red-800 text-xs font-bold disabled:opacity-50">停止此订单全部自动化</button>}
        </div>
      </article>
    ))}
    {pendingTasks.map(/* issue 是当前延期死信，订单关联来自经过验证的任务快照。 */ issue => (
      <article key={`task-${issue.id}`} className="rounded-xl border border-red-100 bg-white p-4 space-y-3">
        <div className="font-bold text-gray-900">任务：{deferredTaskLabel(issue.trigger_type)} <span className="text-xs text-gray-500">· 延期任务 #{issue.id}</span></div>
        <IssueIdentity issue={issue} />
        <div className="text-sm">已尝试执行 {issue.attempt_count} 次；已达到自动重试上限</div>
        <div className="text-sm text-red-700 whitespace-pre-wrap break-words">停止原因：{issue.error_message || '未提供具体原因，请核对实际结果'}</div>
        <div className="flex flex-wrap gap-2">
          <button disabled={busy} onClick={/* 当前回调只重新入队此延期任务。 */ () => onResolveDeferredTask(issue.id, 'retry', issue)} className="px-3 py-2 rounded-lg bg-amber-100 text-amber-800 text-xs font-bold disabled:opacity-50">重新入队</button>
          <button disabled={busy} onClick={/* 当前回调彻底删除单条出错延期任务，不影响未来任务。 */ () => onResolveDeferredTask(issue.id, 'dismiss', issue)} className="px-3 py-2 rounded-lg bg-red-100 text-red-800 text-xs font-bold disabled:opacity-50">彻底删除</button>
          {issue.order_id && issue.can_stop_order && <button disabled={busy} onClick={/* 当前回调停用可靠关联订单，不能为无订单任务猜测归属。 */ () => onResolveDeferredTask(issue.id, 'stop_order', issue)} className="px-3 py-2 rounded-lg bg-red-100 text-red-800 text-xs font-bold disabled:opacity-50">停止此订单全部自动化</button>}
        </div>
      </article>
    ))}
  </section>
);
