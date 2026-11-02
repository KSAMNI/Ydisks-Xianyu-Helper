// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { Item, ItemGreetingRule } from './api';
import { clearItemGreetingRecords, deleteItemGreeting, getItemGreetings, getItems, saveItemGreeting } from './api';
import { emptyItemGreetingDraft, itemGreetingErrorMessage, useItemGreetings } from './itemGreetings';

vi.mock('./api', /* itemGreetingApiMockFactory 提供首次回复 Hook 的确定性 API 替身。 */ () => ({
  clearItemGreetingRecords: vi.fn(),
  deleteItemGreeting: vi.fn(),
  getItemGreetings: vi.fn(),
  getItems: vi.fn(),
  saveItemGreeting: vi.fn(),
}));

// listMock、itemsMock、saveMock、deleteMock、clearMock 是各 API 的可控替身。
const listMock = vi.mocked(getItemGreetings);
// itemsMock 是商品候选读取的可控替身。
const itemsMock = vi.mocked(getItems);
// saveMock 是规则创建与更新的可控替身。
const saveMock = vi.mocked(saveItemGreeting);
// deleteMock 是规则删除的可控替身。
const deleteMock = vi.mocked(deleteItemGreeting);
const clearMock = vi.mocked(clearItemGreetingRecords);

// ruleFixture 是账号 a1 下覆盖 i1 的启用规则。
const ruleFixture: ItemGreetingRule = { id: 1, cookie_id: 'a1', name: '欢迎', enabled: true, reply_content: '你好', reply_image_url: '', reply_image_path: '', item_ids: ['i1'] };
// itemFixture 是编辑器商品候选。
const itemFixture = { id: 1, cookie_id: 'a1', item_id: 'i1', item_title: '商品' } as Item;

// deferred 创建可手动决定完成时机的 Promise，用于构造过期响应。
const deferred = <T,>() => {
  // resolve 在测试需要时完成挂起的 Promise。
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  // promise 是由 resolve 与 reject 手动控制完成时机的挂起请求。
  const promise = new Promise<T>(/* executor 暴露 resolve 与 reject。 */ (ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
};

describe('useItemGreetings', /* 当前回调覆盖首次回复 Hook 的成功、失败、切换和重复提交。 */ () => {
  beforeEach(/* 当前回调重置替身为成功默认值。 */ () => {
    vi.clearAllMocks();
    listMock.mockResolvedValue([ruleFixture]);
    itemsMock.mockResolvedValue([itemFixture]);
    saveMock.mockResolvedValue(ruleFixture);
    deleteMock.mockResolvedValue({ success: true } as never);
    clearMock.mockResolvedValue({ success: true } as never);
  });

  test('按账号加载规则，失败时暴露错误且不清空已有列表', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useItemGreetings('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.rules).toEqual([ruleFixture]));
    expect(listMock).toHaveBeenCalledWith('a1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    listMock.mockRejectedValueOnce(new Error('网络错误'));
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { await result.current.reload(); });
    expect(result.current.loadError).toBe('网络错误');
    expect(result.current.rules).toEqual([ruleFixture]);
  });

  test('切换账号后丢弃旧账号的过期响应', /* 当前回调执行该行为用例。 */ async () => {
    // slow 是故意延迟完成的请求，用于构造过期响应。
    const slow = deferred<ItemGreetingRule[]>();
    listMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slow.promise);
    // result、rerender 提供 Hook 的最新返回值和重新渲染入口。
    const { result, rerender } = renderHook(/* 当前回调渲染被测 Hook。 */ ({ id }) => useItemGreetings(id), { initialProps: { id: 'a1' } });
    listMock.mockResolvedValueOnce([{ ...ruleFixture, id: 2, cookie_id: 'a2' }]);
    rerender({ id: 'a2' });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.rules[0]?.id).toBe(2));
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { slow.resolve([ruleFixture]); await slow.promise; });
    expect(result.current.rules[0]?.id).toBe(2);
  });

  test('新建保存校验必填项，成功后关闭编辑器并刷新', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useItemGreetings('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(listMock).toHaveBeenCalled());
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.openCreate('a1'));
    expect(result.current.draft).toEqual(emptyItemGreetingDraft('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.draftItems).toEqual([itemFixture]));
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { await result.current.save(); });
    expect(result.current.toast?.text).toBe('请至少选择一个商品');
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.setDraft({ ...emptyItemGreetingDraft('a1'), item_ids: ['i1'] }));
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { await result.current.save(); });
    expect(result.current.toast?.text).toBe('回复文字和图片至少填写一项');
    expect(saveMock).not.toHaveBeenCalled();
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.setDraft({ ...emptyItemGreetingDraft('a1'), item_ids: ['i1'], reply_content: '你好' }));
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { await result.current.save(); });
    expect(saveMock).toHaveBeenCalledTimes(1);
    expect(result.current.draft).toBeNull();
    expect(result.current.toast).toEqual({ type: 'success', text: '保存成功' });
    expect(listMock.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  test('商品冲突保留编辑器并提示冲突商品', /* 当前回调执行该行为用例。 */ async () => {
    saveMock.mockRejectedValueOnce(Object.assign(new Error('冲突'), { status: 409, details: { item_ids: ['i2', 'i3'] } }));
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useItemGreetings('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(listMock).toHaveBeenCalled());
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.openEdit(ruleFixture));
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { await result.current.save(); });
    expect(result.current.draft).not.toBeNull();
    expect(result.current.toast).toEqual({ type: 'error', text: '部分商品已被其他首次回复规则使用：i2、i3' });
    expect(result.current.submitState.result).toBe('failure');
  });

  test('关闭编辑器后到达的商品响应不会回填', /* 当前回调执行该行为用例。 */ async () => {
    // slow 是故意延迟完成的请求，用于构造过期响应。
    const slow = deferred<Item[]>();
    itemsMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slow.promise);
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useItemGreetings('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(listMock).toHaveBeenCalled());
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.openCreate('a1'));
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.closeEditor());
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { slow.resolve([itemFixture]); await slow.promise; });
    expect(result.current.draftItems).toEqual([]);
  });

  test('商品候选加载失败时提示且保持可用', /* 当前回调执行该行为用例。 */ async () => {
    itemsMock.mockRejectedValueOnce(new Error('boom'));
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useItemGreetings('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(listMock).toHaveBeenCalled());
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.openCreate('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.toast?.text).toBe('加载商品列表失败'));
    expect(result.current.draft).not.toBeNull();
  });

  test('写操作进行中拒绝重复提交', /* 当前回调执行该行为用例。 */ async () => {
    // slow 是故意延迟完成的请求，用于构造过期响应。
    const slow = deferred<ItemGreetingRule>();
    saveMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slow.promise);
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useItemGreetings('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(listMock).toHaveBeenCalled());
    // first 保存第一次写操作的完成结果。
    let first!: Promise<boolean>;
    // second 保存进行中再次提交得到的返回值，应为 false。
    let second!: boolean;
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => {
      first = result.current.toggle(ruleFixture);
      second = await result.current.toggle(ruleFixture);
    });
    expect(second).toBe(false);
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { slow.resolve(ruleFixture); await first; });
    expect(saveMock).toHaveBeenCalledTimes(1);
    expect(saveMock).toHaveBeenCalledWith({ ...ruleFixture, enabled: false });
  });

  test('删除与清空记录成功或失败都给出提示', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useItemGreetings('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(listMock).toHaveBeenCalled());
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { await result.current.remove(1); });
    expect(deleteMock).toHaveBeenCalledWith(1);
    expect(result.current.toast?.text).toBe('删除成功');
    clearMock.mockRejectedValueOnce(new Error('清理失败'));
    await act(/* 当前回调在 act 中触发 Hook 异步动作并等待完成。 */ async () => { await result.current.clearRecords('a1'); });
    expect(result.current.toast).toEqual({ type: 'error', text: '清理失败' });
    act(/* 当前回调在 act 中触发 Hook 同步动作。 */ () => result.current.dismissToast());
    expect(result.current.toast).toBeNull();
  });
  test('纯资源图片可保存且冲突来源被拒绝，失败时保留图片草稿', /* 覆盖本地图必填校验、互斥和失败重试。 */ async () => {
    // result 提供真实 Hook 状态，API 使用可控替身。
    const { result } = renderHook(/* 渲染账号 a1 的首次回复。 */ () => useItemGreetings('a1'));
    await waitFor(/* 等待初始读取完成。 */ () => expect(result.current.loading).toBe(false));
    act(/* 编辑一条只有本地图的规则。 */ () => result.current.openEdit({ ...ruleFixture, reply_content: '', reply_image_path: '_library/guide.png' }));
    expect(result.current.draft?.imageSource).toBe('local');
    act(/* 注入错误双来源，验证提交保护而不是依赖界面隐藏。 */ () => result.current.setDraft(/* current 是已打开的本地图片草稿。 */ current => current ? { ...current, reply_image_url: 'https://example.test/a.png' } : null));
    await act(/* 发起校验，应在请求前停止。 */ async () => { await result.current.save(); });
    expect(saveMock).not.toHaveBeenCalled();
    expect(result.current.toast?.text).toContain('只能选择一种');
    act(/* 移除冲突的远程来源。 */ () => result.current.setDraft(/* current 保留本地图和商品集合。 */ current => current ? { ...current, reply_image_url: '' } : null));
    saveMock.mockRejectedValueOnce(new Error('保存失败'));
    await act(/* 首次保存故意失败。 */ async () => { await result.current.save(); });
    expect(result.current.draft?.reply_image_path).toBe('_library/guide.png');
    expect(result.current.submitState.result).toBe('failure');
    await act(/* 重试同一纯图片规则。 */ async () => { await result.current.save(); });
    expect(saveMock).toHaveBeenLastCalledWith(expect.objectContaining({ reply_content: '', reply_image_url: '', reply_image_path: '_library/guide.png' }));
    expect(result.current.draft).toBeNull();
  });

  test('旧保存迟到不会关闭新草稿，账号切换清空旧图片选择', /* 覆盖取消、重新编辑与跨账号的草稿生命周期。 */ async () => {
    // slow 让首次保存保持在途，模拟用户在响应前关闭弹窗。
    const slow = deferred<ItemGreetingRule>();
    saveMock.mockReturnValueOnce(slow.promise);
    // result、rerender 控制账号筛选与当前草稿。
    const { result, rerender } = renderHook(/* id 是当前账号筛选。 */ ({ id }) => useItemGreetings(id), { initialProps: { id: 'a1' } });
    await waitFor(/* 等待初始列表完成。 */ () => expect(result.current.loading).toBe(false));
    act(/* 打开原规则。 */ () => result.current.openEdit(ruleFixture));
    // saving 保存旧编辑器的异步完成句柄。
    let saving!: Promise<void>;
    act(/* 启动但暂不完成保存。 */ () => { saving = result.current.save(); });
    act(/* 关闭旧编辑器并选择新的本地图片规则。 */ () => {
      result.current.closeEditor();
      result.current.openEdit({ ...ruleFixture, id: 2, reply_image_path: '_library/new.png' });
    });
    await act(/* 完成旧请求并等待其收尾。 */ async () => { slow.resolve(ruleFixture); await saving; });
    expect(result.current.draft?.id).toBe(2);
    expect(result.current.draft?.reply_image_path).toBe('_library/new.png');
    rerender({ id: 'a2' });
    expect(result.current.draft).toBeNull();
    expect(result.current.draftItems).toEqual([]);
  });
});

describe('itemGreetingErrorMessage', /* 当前回调覆盖错误文案归一。 */ () => {
  test('非冲突错误回退到消息或通用文案', /* 当前回调执行该行为用例。 */ () => {
    expect(itemGreetingErrorMessage(new Error('x'))).toBe('x');
    expect(itemGreetingErrorMessage({})).toBe('操作失败');
    expect(itemGreetingErrorMessage({ status: 409, message: '冲突' })).toBe('冲突');
  });
});
