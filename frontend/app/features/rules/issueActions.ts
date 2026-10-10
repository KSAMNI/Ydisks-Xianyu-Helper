import { useEffect, useRef, useState } from 'react';
import { resolveAutomationRun, resolveDeferredAutomationTask, type AutomationRunIssue, type DeferredAutomationIssue } from './api';
import { issueResolutionPrompt, type AutomationResolution, type DeferredResolution } from './issueState';

/** useIssueActions 拥有人工决策提交状态；同页串行提交，账号切换或卸载后丢弃旧响应，不撤销已到达服务端的停用。 */
export const useIssueActions = (accountID: string, reload: () => Promise<void>) => {
  /** issueActionPending 是短暂提交状态；setPending 仅由当前代次的请求更新。 */
  const [issueActionPending, setPending] = useState(false);
  /** active 保存当前请求代次，既防重复点击也使旧 finally 无权清除新请求状态。 */
  const active = useRef<symbol | null>(null);
  /** currentAccount 保存本次渲染账号，防止切换后、effect 执行前的旧响应刷新旧列表。 */
  const currentAccount = useRef(accountID);
  currentAccount.current = accountID;
  /** 当前副作用在切换账号时清除提交状态，cleanup 撤销响应更新权，但不宣称撤回服务端操作。 */
  useEffect(() => {
    active.current = null;
    setPending(false);
    /** 清理函数让卸载或账号切换后的晚到结果失效。 */
    return () => { active.current = null; };
  }, [accountID]);

  /** resolve 由用户确认后提交 perform；prompt 包含实际处理范围，最新响应才可刷新或提示失败。 */
  const resolve = async (prompt: string, perform: () => Promise<unknown>): Promise<void> => {
    if (active.current || !window.confirm(prompt)) return;
    /** token 是本次提交独享的代次；origin 是用户点击时选中的账号。 */
    const token = Symbol('issue-resolution'), origin = accountID;
    active.current = token;
    setPending(true);
    try {
      await perform();
      if (active.current === token && currentAccount.current === origin) await reload();
    } catch (/* error 是操作或刷新失败，不在旧账号页面弹出过期提示。 */ error) {
      if (active.current === token && currentAccount.current === origin) window.alert('处理失败：' + (error instanceof Error ? error.message : '请刷新后重试'));
    } finally {
      if (active.current === token && currentAccount.current === origin) { active.current = null; setPending(false); }
    }
  };
  /** handleResolveRunIssue 处理运行异常；整单停用必须携带后端确认过的订单身份。 */
  const handleResolveRunIssue = async (id: number, resolution: AutomationResolution, issue?: AutomationRunIssue): Promise<void> => {
    if (resolution === 'stop_order' && (!issue?.can_stop_order || !issue.order_id)) return;
    await resolve(issueResolutionPrompt(resolution, issue), /* 当前回调提交运行主键和动作，不允许客户端指定其他订单。 */ () => resolveAutomationRun(id, resolution));
  };
  /** handleResolveDeferredIssue 处理延期异常；缺少可靠订单时仅允许重试或忽略单任务。 */
  const handleResolveDeferredIssue = async (id: number, resolution: DeferredResolution, issue?: DeferredAutomationIssue): Promise<void> => {
    if (resolution === 'stop_order' && (!issue?.can_stop_order || !issue.order_id)) return;
    await resolve(issueResolutionPrompt(resolution, issue), /* 当前回调只提交延期主键，由服务端重新验证其订单身份。 */ () => resolveDeferredAutomationTask(id, resolution));
  };
  return { issueActionPending, handleResolveRunIssue, handleResolveDeferredIssue };
};

/** copyAutomationOrderID 仅响应用户复制操作；剪贴板不可用时明确提示可手动复制，不静默失败。 */
export const copyAutomationOrderID = async (orderID: string): Promise<void> => {
  try { await navigator.clipboard.writeText(orderID); }
  catch { window.alert('无法访问剪贴板，请手动选择并复制订单号。'); }
};
