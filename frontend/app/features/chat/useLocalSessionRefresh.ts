import { useCallback, useEffect, useRef } from 'react';
import { getChatSessionPage, type ChatSession } from './api';

/** LocalRefreshEntry 保存单账号本地补读的定时器、在途请求与一次补偿预算，由所属Hook清理。 */
type LocalRefreshEntry = {
  /** timer 是尚未触发的300毫秒合并窗口，无定时任务时为undefined。 */
  timer: ReturnType<typeof setTimeout> | undefined;
  /** controller 仅控制当前本地读取，永远不会取消平台同步或其他账号请求。 */
  controller: AbortController | undefined;
  /** dirty 标识本次请求期间又收到事件，需要在完成后最多再补读一次。 */
  dirty: boolean;
  /** remaining 是当前事件批次剩余的补偿读取预算，防止事件流制造无限循环。 */
  remaining: number;
};

/** LocalRefreshActions 暴露本地补读排程及取消能力，不包含任何平台刷新入口。 */
type LocalRefreshActions = {
  /** scheduleLocalRefresh 合并accountID的未知会话事件，不传递任何平台凭证。 */
  scheduleLocalRefresh: (accountID: string) => void;
  /** cancelLocalRefresh 清理全部定时器并取消在途请求，使账号切换或删除前的响应失效。 */
  cancelLocalRefresh: () => void;
};

/** useLocalSessionRefresh 按账号合并本地补读，将仍有效的sessions交给applyPage；卸载会取消全部请求。
 * applyPage只合并展示缓存，不改分页游标；请求固定refresh=false，任何失败均保留已有聊天数据。
 */
export function useLocalSessionRefresh(applyPage: (accountID: string, sessions: ChatSession[]) => void): LocalRefreshActions {
  /** entries 是本Hook拥有的账号任务表，对象身份同时作为迟到响应的代次保护。 */
  const entries = useRef(new Map<string, LocalRefreshEntry>());
  /** mounted 防止已卸载的事件回调重新建立定时任务，StrictMode重新挂载时恢复。 */
  const mounted = useRef(true);
  /** cancelLocalRefresh 先清空代次，再终止I/O；忽略AbortSignal的旧响应也无法提交。 */
  const cancelLocalRefresh = useCallback(/* 无参数清理回调只操作本Hook拥有的资源。 */ () => {
    /** pending 是清理前的任务快照，防止异步完成回调影响遍历。 */
    const pending = [...entries.current.values()];
    entries.current.clear();
    for (const /* entry是当前需要释放的账号本地读取任务。 */ entry of pending) {
      clearTimeout(entry.timer);
      entry.controller?.abort();
    }
  }, []);

  useEffect(/* 当前副作用拥有挂载状态与所有合并任务，清理后禁止迟到写入。 */ () => {
    mounted.current = true;
    return /* 清理回调在卸载时释放定时器和请求，不留下后台轮询。 */ () => {
      mounted.current = false;
      cancelLocalRefresh();
    };
  }, [cancelLocalRefresh]);

  /** scheduleLocalRefresh 对accountID建立一个300毫秒窗口，窗口内事件不追加请求。 */
  const scheduleLocalRefresh = useCallback(/* accountID定位独立账号的本地会话缓存。 */ (accountID: string) => {
    if (!mounted.current || !accountID) return;
    /** existing 是该账号当前的合并窗口或在途读取；只在请求中记录一次补偿需求。 */
    const existing = entries.current.get(accountID);
    if (existing) {
      if (existing.controller) existing.dirty = true;
      return;
    }
    /** entry保存当前批次唯一的身份标记，取消后即使新批次同账号也不会混用。 */
    const entry: LocalRefreshEntry = { timer: undefined, controller: undefined, dirty: false, remaining: 1 };
    entries.current.set(accountID, entry);
    /** run执行当前批次的一次本地读取；后续最多一次补偿，不递归查询平台。 */
    const run = async (): Promise<void> => {
      if (entries.current.get(accountID) !== entry) return;
      entry.timer = undefined;
      entry.dirty = false;
      /** controller只属于本次本地请求，结束后被释放且不影响其他账号。 */
      const controller = new AbortController();
      entry.controller = controller;
      try {
        /** page读取本地首页，必须由调用方合并而不是替换已加载的多页列表。 */
        const page = await getChatSessionPage(accountID, undefined, { signal: controller.signal }, false);
        if (entries.current.get(accountID) === entry && !controller.signal.aborted) applyPage(accountID, page.sessions);
      } catch {
        // 可选补读失败时保留旧会话；下一次用户操作或事件可重试，不触发平台刷新。
      } finally {
        if (entries.current.get(accountID) === entry) {
          entry.controller = undefined;
          if (entry.dirty && entry.remaining > 0) {
            entry.remaining -= 1;
            entry.timer = setTimeout(run, 300);
          } else {
            entries.current.delete(accountID);
          }
        }
      }
    };
    entry.timer = setTimeout(run, 300);
  }, [applyPage]);

  return { scheduleLocalRefresh, cancelLocalRefresh };
}
