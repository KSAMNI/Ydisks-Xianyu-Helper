import { beforeEach, describe, expect, test, vi } from 'vitest';

// 契约客户端与共享 HTTP 客户端替身，用于记录适配器构造的请求。
const { getMock, postMock, putMock, deleteMock, getBlobMock } = vi.hoisted(/* clientMocksFactory 创建 HTTP 方法替身。 */ () => ({ getMock: vi.fn(), postMock: vi.fn(), putMock: vi.fn(), deleteMock: vi.fn(), getBlobMock: vi.fn() }));

vi.mock('../../../shared/api-contract/client', /* contractModuleFactory 提供聊天 API 适配所需的契约依赖。 */ () => ({
  // contractClient 暴露聊天适配器用到的方法。
  contractClient: { GET: getMock, POST: postMock, PUT: putMock, DELETE: deleteMock },
  // contractMultipartBody 原样返回 FormData。
  contractMultipartBody: <T>(form: FormData) => form as unknown as T,
  // runContractRequest 执行请求回调并返回其 data，同时把控制选项记录到 lastOptions。
  runContractRequest: /* executeMock 执行适配器传入的请求动作。 */ async (/* execute 是适配器构造的请求动作。 */ execute: (signal: AbortSignal) => Promise<{ /** data 是成功响应体。 */ data?: unknown }>, /* options 是调用方传入的超时与取消选项。 */ options?: unknown) => {
    lastOptions.value = options;
    return (await execute(new AbortController().signal)).data;
  },
}));

vi.mock('../../../shared/http/client', /* httpClientModuleFactory 保留真实 ApiError 语义，只替换二进制读取。 */ async importOriginal => {
  // actual 是共享 HTTP 客户端的真实导出集合。
  const actual = await importOriginal<typeof import('../../../shared/http/client')>();
  return { ...actual, getBlob: getBlobMock };
});

// lastOptions 记录最近一次请求收到的控制选项，供超时断言使用。
const lastOptions: { /** value 是最近一次的请求控制选项。 */ value: unknown } = { value: undefined };

import { ApiError } from '../../../shared/http/client';
import * as api from './api';

// validMessage 是满足展示模型全部必填字段的出站消息。
const validMessage = { id: 1, account_id: 'a', chat_id: 'c', message_key: 'k', direction: 'outgoing', sender_id: 's', sender_name: 'n', message_type: 'text', content: 'hi', status: 'sending', sent_at: 1 };

describe('聊天 API 适配器', /* 聊天 API 测试覆盖请求映射、响应转换与错误消息提取。 */ () => {
  beforeEach(/* 当前回调重置契约客户端替身与选项记录。 */ () => {
    vi.clearAllMocks();
    lastOptions.value = undefined;
    // mock 是逐个重置默认响应的契约方法替身。
    for (const /* mock 是逐个重置默认响应的契约方法替身。 */ mock of [getMock, postMock, putMock, deleteMock]) mock.mockResolvedValue({ data: {} });
  });

  test('账号摘要只保留非敏感字段', /* 当前回调验证 DTO 转换丢弃额外字段。 */ async () => {
    getMock.mockResolvedValue({ data: [{ id: 'a', enabled: true, remark: 'r', cookie_value: 'secret' }] });
    // result 是转换后的账号摘要。
    const result = await api.getAccountDetails();
    expect(result[0].id).toBe('a');
    expect(result[0]).not.toHaveProperty('cookie_value');
  });

  test('运行状态与会话分页透传响应并按 refresh 放宽超时', /* 当前回调验证 refresh 使用 60 秒超时与游标参数。 */ async () => {
    getMock.mockResolvedValue({ data: { sessions: [{ chat_id: 'c' }], has_more: false } });
    await api.getAccountRuntimeStatuses();
    await api.getChatSessionPage('a', 3, { timeoutMs: 5 }, true, 'sc');
    expect(lastOptions.value).toMatchObject({ timeoutMs: 60_000 });
    expect(getMock).toHaveBeenLastCalledWith('/api/v1/chat/sessions', expect.objectContaining({ params: { query: { account_id: 'a', cursor: 3, stored_cursor: 'sc', refresh: 1 } } }));
    await api.getChatSessionPage('a', undefined, { timeoutMs: 5 });
    expect(lastOptions.value).toMatchObject({ timeoutMs: 5 });
    expect(await api.getChatSessions('a')).toEqual([{ chat_id: 'c' }]);
  });

  test('删除会话、快捷回复与已读上报映射到对应路径', /* 当前回调验证各写操作的参数结构。 */ async () => {
    await api.deleteChatSession('a', 'c');
    expect(deleteMock).toHaveBeenCalledWith('/api/v1/chat/sessions', expect.objectContaining({ params: { query: { account_id: 'a', chat_id: 'c' } } }));
    await api.deleteChatQuickReply('a', 9);
    expect(deleteMock).toHaveBeenLastCalledWith('/api/v1/chat/quick-replies/{quick_reply_id}', expect.objectContaining({ params: { path: { quick_reply_id: 9 }, query: { account_id: 'a' } } }));
    await api.markChatRead('a', 'c', [{ messageId: 'm', sessionId: 's', cid: 'x', conversationType: 1, extra: 'drop' } as never]);
    expect(postMock).toHaveBeenLastCalledWith('/api/v1/chat/read', expect.objectContaining({ body: { account_id: 'a', chat_id: 'c', message_ids: [{ messageId: 'm', sessionId: 's', cid: 'x', conversationType: 1 }] } }));
  });

  test('消息分页与列表读取传递游标', /* 当前回调验证 beforeId 与 cursor 的参数映射。 */ async () => {
    getMock.mockResolvedValue({ data: { messages: [validMessage], has_more: false } });
    expect(await api.getChatMessages('a', 'c', 5)).toEqual([validMessage]);
    expect(getMock).toHaveBeenCalledWith('/api/v1/chat/messages', expect.objectContaining({ params: { query: { account_id: 'a', chat_id: 'c', cursor: undefined, before_id: 5 } } }));
  });

  test('快捷回复与买家备注响应被转换为 UI 模型', /* 当前回调验证列表、创建、读取和保存四个转换。 */ async () => {
    getMock.mockResolvedValueOnce({ data: { quick_replies: [{ id: 1, account_id: 'a', content: 'x', created_at: 't', extra: 1 }] } });
    expect(await api.getChatQuickReplies('a')).toEqual([{ id: 1, account_id: 'a', content: 'x', created_at: 't' }]);
    postMock.mockResolvedValueOnce({ data: { id: 2, account_id: 'a', content: 'y', created_at: 't', extra: 1 } });
    expect(await api.createChatQuickReply('a', 'y')).toEqual({ id: 2, account_id: 'a', content: 'y', created_at: 't' });
    // note 是买家备注 DTO。
    const note = { account_id: 'a', buyer_id: 'b', content: 'n', updated_at: 't' };
    getMock.mockResolvedValueOnce({ data: { ...note, extra: 1 } });
    expect(await api.getChatBuyerNote('a', 'b')).toEqual(note);
    putMock.mockResolvedValueOnce({ data: { ...note, extra: 1 } });
    expect(await api.saveChatBuyerNote('a', 'b', 'n')).toEqual(note);
  });

  test('文本消息与商品卡片发送构造请求体', /* 当前回调验证文本体原样提交且商品卡片只带展示字段。 */ async () => {
    await api.sendChatMessage({ account_id: 'a', chat_id: 'c', peer_user_id: 'p', text: 't' });
    expect(postMock).toHaveBeenLastCalledWith('/api/v1/chat/messages', expect.objectContaining({ body: { account_id: 'a', chat_id: 'c', peer_user_id: 'p', text: 't' } }));
    await api.sendChatItemCard('a', 'c', { item_id: 'i', title: 't', image_url: 'u', price: '1', description: 'drop' } as never);
    expect(postMock).toHaveBeenLastCalledWith('/api/v1/chat/item-cards', expect.objectContaining({ body: { account_id: 'a', chat_id: 'c', item: { item_id: 'i', title: 't', image_url: 'u', price: '1' } } }));
  });

  test('图片发送只提交非空可选字段并放宽超时', /* 当前回调验证 FormData 内容与 120 秒默认超时。 */ async () => {
    // image 是待发送的最小图片文件。
    const image = new File(['x'], 'a.png');
    await api.sendChatImage({ account_id: 'a', chat_id: 'c', peer_user_id: 'p', peer_name: 'N', item_id: '', image });
    // form 是适配器传给契约客户端的表单。
    const form = (postMock.mock.calls[0][1] as { /** body 是 multipart 表单。 */ body: FormData }).body;
    expect(form.get('peer_name')).toBe('N');
    expect(form.has('item_id')).toBe(false);
    expect(form.has('peer_avatar_url')).toBe(false);
    expect(form.get('image')).toBeInstanceOf(File);
    expect(lastOptions.value).toMatchObject({ timeoutMs: 120_000 });
  });

  test('商品查询空关键字不提交 query 并转换商品字段', /* 当前回调验证分页结果的字段白名单。 */ async () => {
    getMock.mockResolvedValue({ data: { items: [{ item_id: 'i', title: 't', image_url: 'u', price: '1', description: 'd', extra: 1 }], page: 2, has_more: true } });
    // result 是转换后的商品分页。
    const result = await api.getChatItems('a', 'c', 'peer', '', 2);
    expect(result).toEqual({ items: [{ item_id: 'i', title: 't', image_url: 'u', price: '1', description: 'd' }], page: 2, has_more: true });
    expect(getMock).toHaveBeenCalledWith('/api/v1/chat/items', expect.objectContaining({ params: { query: { account_id: 'a', chat_id: 'c', role: 'peer', query: undefined, page: 2 } } }));
  });

  test('待确认与已发送错误只在错误码和消息结构合法时提取', /* 当前回调覆盖错误类型、错误码、详情缺失和字段非法分支。 */ () => {
    // build 构造携带指定错误码和 outgoing_message 的 ApiError。
    const build = (/* code 是服务端错误码。 */ code: string, /* message 是错误详情中的消息。 */ message?: unknown) => new ApiError(409, { code, message: 'm', request_id: 'r', details: message === undefined ? undefined : { outgoing_message: message } });
    // code 是会产生待确认外发消息的服务端错误码。
    for (const /* code 是会产生待确认外发消息的服务端错误码。 */ code of ['chat_send_uncertain', 'chat_image_send_uncertain', 'chat_item_card_send_uncertain']) {
      expect(api.uncertainOutgoingMessageFromError(build(code, validMessage))?.status).toBe('uncertain');
    }
    expect(api.uncertainOutgoingMessageFromError(new Error('x'))).toBeUndefined();
    expect(api.uncertainOutgoingMessageFromError(build('other', validMessage))).toBeUndefined();
    expect(api.uncertainOutgoingMessageFromError(build('chat_send_uncertain'))).toBeUndefined();
    expect(api.uncertainOutgoingMessageFromError(build('chat_send_uncertain', null))).toBeUndefined();
    expect(api.uncertainOutgoingMessageFromError(build('chat_send_uncertain', { ...validMessage, status: 'bogus' }))).toBeUndefined();
    expect(api.confirmedOutgoingMessageFromError(build('chat_send_status_save_failed', validMessage))?.status).toBe('sent');
    expect(api.confirmedOutgoingMessageFromError(new Error('x'))).toBeUndefined();
    expect(api.confirmedOutgoingMessageFromError(build('chat_send_uncertain', validMessage))).toBeUndefined();
    expect(api.confirmedOutgoingMessageFromError(build('chat_send_status_save_failed', 5))).toBeUndefined();
  });

  test('关键词回复只导入文本模板并按正文去重', /* 当前回调验证图片型、空正文、重复正文与非数组响应的处理。 */ async () => {
    // rows 是包含文本、空正文、重复正文和图片型规则的响应集合。
    const rows = [
      { id: 1, keyword: 'k1', expressions: ['k1', 'k2'], match_type: 'contains', reply: '  你好  ', item_id: '', item_ids: [], type: 'text', image_url: '' },
      { id: 2, keyword: 'k3', expressions: ['k3'], match_type: 'contains', reply: '   ', item_id: '', item_ids: [], type: 'text', image_url: '' },
      { id: 3, keyword: 'k4', expressions: ['k4'], match_type: 'contains', reply: '你好', item_id: '', item_ids: [], type: 'text', image_url: '' },
      { id: 4, keyword: 'k5', expressions: ['k5'], match_type: 'contains', reply: '图片话术', item_id: '', item_ids: [], type: 'image', image_url: 'https://img.example/a.png' },
    ];
    getMock.mockResolvedValueOnce({ data: rows });
    expect(await api.getChatKeywordReplies('acc')).toEqual([{ id: '1', keyword: 'k1', content: '你好' }]);
    expect(getMock).toHaveBeenCalledWith('/api/v1/reply-rules/{cid}/typed', expect.objectContaining({ params: { path: { cid: 'acc' } } }));
    getMock.mockResolvedValueOnce({ data: { data: rows } });
    expect(await api.getChatKeywordReplies('acc')).toEqual([]);
  });

  test('资源中心图片读取转换为可发送文件并校验类型与体积', /* 当前回调验证资源中心图片读取的成功与三类失败分支。 */ async () => {
    getBlobMock.mockResolvedValueOnce(new Blob([new Uint8Array([1, 2, 3])], { type: 'image/png' }));
    // file 是由资源中心图片内容转换出的待发送文件。
    const file = await api.loadChatLibraryImage({ id: 7, name: '话术图' });
    expect(file.name).toBe('话术图.png');
    expect(file.type).toBe('image/png');
    expect(file.size).toBe(3);
    expect(getBlobMock).toHaveBeenCalledWith('/api/v1/image-library/7/content', expect.objectContaining({ timeoutMs: 60_000 }));
    getBlobMock.mockResolvedValueOnce(new Blob([new Uint8Array([1])], { type: 'image/gif' }));
    expect((await api.loadChatLibraryImage({ id: 8, name: '' })).name).toBe('image-8.gif');
    getBlobMock.mockResolvedValueOnce(new Blob(['x'], { type: 'text/plain' }));
    await expect(api.loadChatLibraryImage({ id: 9, name: 'n' })).rejects.toThrow('资源中心图片内容不是图片');
    getBlobMock.mockResolvedValueOnce(new Blob([], { type: 'image/png' }));
    await expect(api.loadChatLibraryImage({ id: 10, name: 'n' })).rejects.toThrow('资源中心图片内容为空');
    getBlobMock.mockResolvedValueOnce({ type: 'image/png', size: 10 * 1024 * 1024 + 1 } as Blob);
    await expect(api.loadChatLibraryImage({ id: 11, name: 'n' })).rejects.toThrow('资源中心图片超过 10 MiB');
  });
});
