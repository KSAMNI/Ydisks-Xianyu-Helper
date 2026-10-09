// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { getChatSessionPage, type ChatSessionPage } from './api';
import { useLocalSessionRefresh } from './useLocalSessionRefresh';

vi.mock('./api', /* API替身仅提供本地会话页，不存在真实平台请求。 */ () => ({ getChatSessionPage: vi.fn() }));
/** getPage记录是否错误传入refresh=true及并发账号隔离。 */
const getPage = vi.mocked(getChatSessionPage);
/** page是最小合法本地页，不含任何敏感字段。 */
const page: ChatSessionPage = { sessions: [], has_more: false };

beforeEach(/* 每个测试使用独立假时钟和API状态，无真实等待。 */ () => {
  vi.useFakeTimers();
  getPage.mockReset().mockResolvedValue(page);
});
afterEach(/* 回收全部虚拟定时器，避免污染其他Hook测试。 */ () => { vi.useRealTimers(); });

/** deferredPage 返回手动完成的本地读取及resolve，用于模拟忽略取消的晚到响应。 */
function deferredPage() {
  /** resolve由Promise构造回调赋值，测试只能以合法会话页结束请求。 */
  let resolve!: (value: ChatSessionPage) => void;
  /** promise在测试明确释放前保持pending，模拟本地数据库响应延迟。 */
  const promise = new Promise<ChatSessionPage>(/* complete是当前延迟请求唯一的完成入口。 */ complete => { resolve = complete; });
  return { promise, resolve };
}

test('事件窗口内多次通知合并为一次本地读取且账号独立', /* 验证300毫秒批次与refresh=false硬边界。 */ async () => {
  /** apply记录仍有效的账号页提交，不替代API请求本身。 */
  const apply = vi.fn();
  /** hook持有独立请求队列，测试结束由unmount释放。 */
  const hook = renderHook(/* 构造被测本地补读Hook。 */ () => useLocalSessionRefresh(apply));
  act(/* 同账号连续事件不能放大本地读取请求，空账号应忽略。 */ () => {
    hook.result.current.scheduleLocalRefresh('');
    for (let /* index代表同一合并窗口的连续通知。 */ index = 0; index < 30; index += 1) hook.result.current.scheduleLocalRefresh('one');
    hook.result.current.scheduleLocalRefresh('two');
  });
  await act(/* 窗口尚未结束时不进行请求。 */ async () => { await vi.advanceTimersByTimeAsync(299); });
  expect(getPage).not.toHaveBeenCalled();
  await act(/* 两个账号在各自窗口到期后各查询一次。 */ async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(getPage).toHaveBeenCalledTimes(2);
  expect(getPage).toHaveBeenCalledWith('one', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }), false);
  expect(getPage).toHaveBeenCalledWith('two', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }), false);
  expect(apply).toHaveBeenCalledTimes(2);
  hook.unmount();
});

test('在途通知最多追加一次补读而非无限刷新', /* 连续消息只能消耗一个补偿预算。 */ async () => {
  /** first和second控制原始请求及唯一一次补偿请求。 */
  const first = deferredPage();
  /** second是补偿请求，期间再来事件也不能触发第三次读取。 */
  const second = deferredPage();
  getPage.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  /** hook只记录有效提交，不依赖真实React状态。 */
  const hook = renderHook(/* 构造不产生额外副作用的资料补读。 */ () => useLocalSessionRefresh(vi.fn()));
  act(/* 开始第一批未知会话补读。 */ () => hook.result.current.scheduleLocalRefresh('one'));
  await act(/* 让第一请求保持在途。 */ async () => { await vi.advanceTimersByTimeAsync(300); });
  act(/* 在途期间反复事件只标记一次dirty。 */ () => {
    hook.result.current.scheduleLocalRefresh('one');
    hook.result.current.scheduleLocalRefresh('one');
  });
  await act(/* 完成首请求并等待补偿窗口。 */ async () => { first.resolve(page); await vi.advanceTimersByTimeAsync(300); });
  expect(getPage).toHaveBeenCalledTimes(2);
  act(/* 补偿请求期间继续收到事件，也不得无限补读。 */ () => hook.result.current.scheduleLocalRefresh('one'));
  await act(/* 消耗全部补偿预算后没有第三个定时请求。 */ async () => { second.resolve(page); await vi.advanceTimersByTimeAsync(1000); });
  expect(getPage).toHaveBeenCalledTimes(2);
  hook.unmount();
});

test('切换取消和卸载拒绝迟到提交且不阻塞新批次', /* 模拟旧请求忽略AbortSignal后仍成功返回。 */ async () => {
  /** oldRequest是取消后仍可能返回的旧读取。 */
  const oldRequest = deferredPage();
  getPage.mockReturnValueOnce(oldRequest.promise);
  /** apply记录新旧代次是否串写。 */
  const apply = vi.fn();
  /** hook为切换和卸载提供显式取消能力。 */
  const hook = renderHook(/* 使用稳定提交回调创建队列。 */ () => useLocalSessionRefresh(apply));
  act(/* 开始旧账号补读。 */ () => hook.result.current.scheduleLocalRefresh('one'));
  await act(/* 请求开始后才能验证在途AbortSignal。 */ async () => { await vi.advanceTimersByTimeAsync(300); });
  /** signal属于被取消的旧读取，之后不得提交其结果。 */
  const signal = getPage.mock.calls[0][2]?.signal;
  act(/* 模拟账号切换，清除旧请求后允许相同账号新批次。 */ () => {
    hook.result.current.cancelLocalRefresh();
    hook.result.current.scheduleLocalRefresh('one');
  });
  expect(signal?.aborted).toBe(true);
  await act(/* 旧结果到达但不得写入，随后新批次可正常提交。 */ async () => { oldRequest.resolve(page); await vi.advanceTimersByTimeAsync(300); });
  expect(apply).toHaveBeenCalledTimes(1);
  act(/* 卸载前留下一个尚未发出的定时任务。 */ () => hook.result.current.scheduleLocalRefresh('two'));
  /** schedule保留卸载前的闭包，验证旧事件回调也不能重建工作。 */
  const schedule = hook.result.current.scheduleLocalRefresh;
  hook.unmount();
  schedule('three');
  await act(/* 清理后的计时器不再发起读取。 */ async () => { await vi.advanceTimersByTimeAsync(1000); });
  expect(getPage).toHaveBeenCalledTimes(2);
});

test('本地读取失败保留原数据并允许下一次事件重试', /* 错误不转成平台刷新也不自动循环。 */ async () => {
  getPage.mockRejectedValueOnce(new Error('local read failed'));
  /** apply在失败时不得收到空页替换通知。 */
  const apply = vi.fn();
  /** hook管理本次失败后释放的任务。 */
  const hook = renderHook(/* 创建可验证失败恢复的队列。 */ () => useLocalSessionRefresh(apply));
  act(/* 发起失败读取。 */ () => hook.result.current.scheduleLocalRefresh('one'));
  await act(/* 错误结束后长时间也不能自动重试。 */ async () => { await vi.advanceTimersByTimeAsync(10000); });
  expect(getPage).toHaveBeenCalledTimes(1);
  expect(apply).not.toHaveBeenCalled();
  act(/* 下一次事件才允许新的本地读取。 */ () => hook.result.current.scheduleLocalRefresh('one'));
  await act(/* 新事件窗口结束后可以成功提交。 */ async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(apply).toHaveBeenCalledWith('one', []);
  hook.unmount();
});

test('已进入事件队列的旧定时回调在取消后仍不得读取', /* 即使计时器已排队无法撤销，对象代次仍阻止外部I/O。 */ async () => {
  /** timers观察调度出的回调，不替代被测本地读取流程。 */
  const timers = vi.spyOn(globalThis, 'setTimeout');
  /** hook管理随后被显式取消的旧任务。 */
  const hook = renderHook(/* 构造稳定的资料提交替身。 */ () => useLocalSessionRefresh(vi.fn()));
  try {
    act(/* 排程一个尚未执行的旧账号任务。 */ () => hook.result.current.scheduleLocalRefresh('one'));
    /** queued代表已经被事件循环取出的旧回调，因此取消计时器也可能来不及撤销。 */
    const queued = timers.mock.calls.at(-1)?.[0];
    act(/* 取消后，任何旧回调都必须重新核对任务身份。 */ () => hook.result.current.cancelLocalRefresh());
    expect(typeof queued).toBe('function');
    await act(/* 手动交付已排队回调，验证实际执行不会访问API。 */ async () => { if (typeof queued === 'function') await queued(); });
    expect(getPage).not.toHaveBeenCalled();
  } finally {
    hook.unmount();
    timers.mockRestore();
  }
});
