import {
AccountDetail,
ChatBuyerNote,
ChatItem,
ChatItemPage,
ChatMessage,
ChatQuickReply,
ChatSession,
OperationResponse
} from './models';
import { contractClient, contractMultipartBody, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import { ApiError } from '../../../shared/http/client';
import { getBlob } from '../../../shared/http/client';
import { imageLibraryContentURL } from '../../../shared/imageLibrary';
export type * from './models';
import type { ChatReadReceipt } from './types';

/** 聊天账号选择器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => {
  // response 是账号摘要 transport DTO 集合，转换后只向聊天 UI 暴露非敏感字段。
  const response = await runContractRequest(/* signal 是本次聊天账号摘要请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);
  return response.map(/* item 是当前待转换的账号摘要 DTO。 */ item => ({
    id: item.id,
    enabled: item.enabled,
    auto_confirm: item.auto_confirm,
    remark: item.remark,
    pause_duration: item.pause_duration,
    paused_until: item.paused_until,
    paused: item.paused,
    username: item.username,
    show_browser: item.show_browser,
    nickname: item.nickname,
    avatar_url: item.avatar_url,
    profile_error: item.profile_error,
  }));
};

/** 聊天运行提示读取账号连接状态索引。 */
export const getAccountRuntimeStatuses = async (options?: RequestControlOptions): Promise<Record<string, { /** 当前连接状态。 */ state: NonNullable<AccountDetail['runtime_state']>; /** 状态说明。 */ message?: string; /** 是否已连接。 */ connected: boolean; /** 连续失败次数。 */ failures: number; /** 最近更新时间。 */ updated_at: string }>> =>
  runContractRequest(/* signal 是本次聊天运行状态请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/accounts/runtime-status', { signal }), options);
export interface ChatSessionPage { /** sessions 表示聊天会话列表。 */ sessions: ChatSession[]; /** has_more 表示任一分页来源仍有更多数据。 */ has_more: boolean; /** next_cursor 表示下一平台页游标。 */ next_cursor?: number; /** next_stored_cursor 表示下一本地缓存页游标。 */ next_stored_cursor?: string; /** platform_has_more 表示平台联系人是否仍有下一页；旧响应缺失时回退 has_more。 */ platform_has_more?: boolean; /** stored_has_more 表示本地缓存联系人是否仍有下一页；旧响应缺失时回退 has_more。 */ stored_has_more?: boolean }

// getChatSessionPage 分页读取聊天会话。
export const getChatSessionPage = async (accountId: string, cursor?: number, options?: RequestControlOptions, refresh = false, storedCursor?: string): Promise<ChatSessionPage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const response = await runContractRequest(
    /* signal 是本次聊天会话分页请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/sessions', {
      params: { query: { account_id: accountId, cursor, stored_cursor: storedCursor, refresh: refresh ? 1 : undefined } },
      signal,
    }),
		{ timeoutMs: refresh ? 60_000 : options?.timeoutMs, signal: options?.signal },
	);
	return response;
};

// getChatSessions 读取聊天会话列表。
export const getChatSessions = async (accountId: string, options?: RequestControlOptions): Promise<ChatSession[]> =>
	(await getChatSessionPage(accountId, undefined, options)).sessions;

/** 在本机隐藏指定会话并物理清空其展示消息。 */
export const deleteChatSession = async (accountId: string, chatId: string, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次会话删除请求的超时与取消控制信号。 */ signal => contractClient.DELETE('/api/v1/chat/sessions', {
    params: { query: { account_id: accountId, chat_id: chatId } },
    signal,
  }), options);

/** 判断未知值是否满足聊天消息展示模型的全部必填字段。 */
const isChatMessage = (value: unknown): value is ChatMessage => {
  if (!value || typeof value !== 'object') return false;
  // candidate 保存待验证的未知错误详情字段。
  const candidate = value as Record<string, unknown>;
  return typeof candidate.id === 'number' && typeof candidate.account_id === 'string' && typeof candidate.chat_id === 'string'
    && typeof candidate.message_key === 'string' && (candidate.direction === 'incoming' || candidate.direction === 'outgoing')
    && typeof candidate.sender_id === 'string' && typeof candidate.sender_name === 'string'
    && ['text', 'image', 'video', 'audio', 'item', 'system'].includes(String(candidate.message_type))
    && typeof candidate.content === 'string' && ['received', 'sending', 'sent', 'failed', 'uncertain'].includes(String(candidate.status))
    && typeof candidate.sent_at === 'number';
};

/** 从结果待确认错误中提取不可自动重试的外发消息。 */
export const uncertainOutgoingMessageFromError = (error: unknown): ChatMessage | undefined => {
	if (!(error instanceof ApiError) || !['chat_send_uncertain', 'chat_image_send_uncertain', 'chat_item_card_send_uncertain'].includes(error.code)) return undefined;
	// outgoingMessage 保存后端错误详情中的待确认消息 DTO。
	const outgoingMessage = error.details?.outgoing_message;
	if (!isChatMessage(outgoingMessage)) return undefined;
	return { ...outgoingMessage, status: 'uncertain' };
};

/** 从“远端已发送但状态收口失败”错误中提取不可重试的外发消息。 */
export const confirmedOutgoingMessageFromError = (error: unknown): ChatMessage | undefined => {
	if (!(error instanceof ApiError) || error.code !== 'chat_send_status_save_failed') return undefined;
  // outgoingMessage 保存后端统一错误详情中的 snake_case 消息 DTO。
  const outgoingMessage = error.details?.outgoing_message;
  if (!isChatMessage(outgoingMessage)) return undefined;
  return { ...outgoingMessage, status: 'sent' };
};

export interface ChatMessagePage {
	/** messages 表示聊天消息列表。 */ messages: ChatMessage[];
	/** has_more 表示是否存在更多数据。 */ has_more: boolean;
	/** next_cursor 表示下一页游标。 */ next_cursor?: number;
	/** session 表示会话。 */ session?: ChatSession;
}

// getChatMessagePage 分页读取聊天消息。
export const getChatMessagePage = async (accountId: string, chatId: string, cursor?: number, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessagePage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const response = await runContractRequest(/* signal 是本次聊天消息分页请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/messages', {
		params: { query: { account_id: accountId, chat_id: chatId, cursor, before_id: beforeId } },
		signal,
	}), options);
	return response;
};

// getChatMessages 读取聊天消息列表。
export const getChatMessages = async (accountId: string, chatId: string, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessage[]> =>
	(await getChatMessagePage(accountId, chatId, undefined, beforeId, options)).messages;

/** 查询当前账号可复用的人工快捷回复。 */
export const getChatQuickReplies = async (accountId: string, options?: RequestControlOptions): Promise<ChatQuickReply[]> => {
  // response 保存契约客户端返回的快捷回复列表 DTO，随后转换为 feature UI 模型。
  const response = await runContractRequest(/* signal 是本次账号快捷回复查询的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/quick-replies', {
    params: { query: { account_id: accountId } },
    signal,
  }), options);
  return response.quick_replies.map(/* reply 是当前待转换的快捷回复 DTO。 */ reply => ({
    id: reply.id,
    account_id: reply.account_id,
    content: reply.content,
    created_at: reply.created_at,
  }));
};

/** 为当前账号新增人工快捷回复。 */
export const createChatQuickReply = async (accountId: string, content: string, options?: RequestControlOptions): Promise<ChatQuickReply> => {
  // response 保存创建接口返回的快捷回复 DTO，转换后不向组件暴露生成类型。
  const response = await runContractRequest(/* signal 是本次新增快捷回复请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/quick-replies', {
    body: { account_id: accountId, content },
    signal,
  }), options);
  return { id: response.id, account_id: response.account_id, content: response.content, created_at: response.created_at };
};

/** 删除当前账号下指定的人工快捷回复。 */
export const deleteChatQuickReply = async (accountId: string, quickReplyId: number, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次删除快捷回复请求的超时与取消控制信号。 */ signal => contractClient.DELETE('/api/v1/chat/quick-replies/{quick_reply_id}', {
    params: { path: { quick_reply_id: quickReplyId }, query: { account_id: accountId } },
    signal,
  }), options);

/** 查询当前账号下指定买家的完整备注；未填写时仍返回空内容。 */
export const getChatBuyerNote = async (accountId: string, buyerId: string, options?: RequestControlOptions): Promise<ChatBuyerNote> => {
  // response 保存契约客户端返回的买家备注 DTO，转换后由聊天 feature 管理。
  const response = await runContractRequest(/* signal 是本次买家备注查询的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/buyer-notes/{buyer_id}', {
    params: { path: { buyer_id: buyerId }, query: { account_id: accountId } },
    signal,
  }), options);
  return { account_id: response.account_id, buyer_id: response.buyer_id, content: response.content, updated_at: response.updated_at };
};

/** 保存当前账号下指定买家的完整备注；空内容会清除已有备注。 */
export const saveChatBuyerNote = async (accountId: string, buyerId: string, content: string, options?: RequestControlOptions): Promise<ChatBuyerNote> => {
  // response 保存保存接口返回的最终买家备注 DTO。
  const response = await runContractRequest(/* signal 是本次买家备注保存请求的超时与取消控制信号。 */ signal => contractClient.PUT('/api/v1/chat/buyer-notes/{buyer_id}', {
    params: { path: { buyer_id: buyerId } },
    body: { account_id: accountId, content },
    signal,
  }), options);
  return { account_id: response.account_id, buyer_id: response.buyer_id, content: response.content, updated_at: response.updated_at };
};

// sendChatMessage 发送聊天文本消息。
export const sendChatMessage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** peer_user_id 表示会话对端标识。 */ peer_user_id: string; /** peer_name 表示会话对端名称。 */ peer_name?: string;
  /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** text 表示文本。 */ text: string;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> =>
  runContractRequest(/* signal 是本次聊天文本发送请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/messages', { body: input, signal }), options);

// sendChatImage 发送聊天图片消息。
export const sendChatImage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** peer_user_id 表示会话对端标识。 */ peer_user_id: string; /** peer_name 表示会话对端名称。 */ peer_name?: string;
  /** peer_avatar_url 表示会话对端头像地址。 */ peer_avatar_url?: string; /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** image 表示图片数据。 */ image: File;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> => {
	// form 保存聊天图片的原生 multipart 请求体，浏览器负责生成正确的 boundary。
	const form = new FormData();
	form.set('account_id', input.account_id);
	form.set('chat_id', input.chat_id);
	form.set('peer_user_id', input.peer_user_id);
	form.set('image', input.image);
	// optionalFields 保存可空展示字段；仅提交非空值，防止 FormData 将 undefined 字符串化后污染聊天元数据。
	const optionalFields = [
		['peer_name', input.peer_name],
		['peer_avatar_url', input.peer_avatar_url],
		['item_id', input.item_id],
		['item_title', input.item_title],
	] as const;
	for (const /* fieldName 和 fieldValue 分别表示可选表单字段名称及其当前用户可见元数据。 */ [fieldName, fieldValue] of optionalFields) {
		if (fieldValue) form.set(fieldName, fieldValue);
	}
	return runContractRequest(/* signal 是本次聊天图片发送请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/images', {
		// body 保持原生 FormData；共享契约层仅提供运行时类型适配，不定义业务传输 DTO。
		body: contractMultipartBody(form),
    signal,
  }), { timeoutMs: 120_000, ...options });
};

/** 查询当前个人会话中对方或当前账号的在售商品。 */
export const getChatItems = async (accountId: string, chatId: string, role: 'peer' | 'self', query: string, page: number, options?: RequestControlOptions): Promise<ChatItemPage> => {
	// response 是 OpenAPI 契约客户端返回的商品分页 DTO。
	const response = await runContractRequest(/* signal 是本次商品查询的超时与取消信号。 */ signal => contractClient.GET('/api/v1/chat/items', {
		params: { query: { account_id: accountId, chat_id: chatId, role, query: query || undefined, page } },
		signal,
	}), options);
	return {
		items: response.items.map(/* item 是当前待转换的商品 transport DTO。 */ item => ({ item_id: item.item_id, title: item.title, image_url: item.image_url, price: item.price, description: item.description })),
		page: response.page,
		has_more: response.has_more,
	};
};

/** 将列表中选定的商品快照发送到当前个人会话。 */
export const sendChatItemCard = async (accountId: string, chatId: string, item: ChatItem, options?: RequestControlOptions): Promise<{ /** message 是本地状态机返回的出站商品消息。 */ message: ChatMessage }> =>
	runContractRequest(/* signal 是本次商品卡片发送的超时与取消信号。 */ signal => contractClient.POST('/api/v1/chat/item-cards', {
		body: { account_id: accountId, chat_id: chatId, item: { item_id: item.item_id, title: item.title, image_url: item.image_url, price: item.price } },
		signal,
	}), options);

/** 向平台确认指定会话中的入站消息已读。 */
export const markChatRead = async (accountId: string, chatId: string, messageIDs: ChatReadReceipt[], options?: RequestControlOptions): Promise<OperationResponse> =>
	runContractRequest(/* signal 是本次聊天已读上报请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/read', {
    body: { account_id: accountId, chat_id: chatId, message_ids: messageIDs.map(/* receipt 是当前待序列化的平台已读回执。 */ receipt => ({ messageId: receipt.messageId, sessionId: receipt.sessionId, cid: receipt.cid, conversationType: receipt.conversationType })) },
    signal,
  }), options);

/** chatImageMaxBytes 与后端聊天单图上限保持一致，避免把必然被拒绝的资源中心图片送进发送流程。 */
const chatImageMaxBytes = 10 * 1024 * 1024;

/** chatImageExtensionByMimeType 把服务端返回的图片 MIME 类型映射为上传文件名后缀。 */
const chatImageExtensionByMimeType: Record<string, string> = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/gif': 'gif', 'image/webp': 'webp' };

/** ChatLibraryImageAsset 描述聊天窗口引用一张资源中心图片所需的最小字段。 */
export type ChatLibraryImageAsset = {
  /** id 是资源中心图片主键，用于拼接受登录保护的内容地址。 */
  id: number;
  /** name 是用户可见的图片名，用作上传文件名的前缀。 */
  name: string;
};

/**
 * loadChatLibraryImage 读取资源中心图片内容并转换为聊天发送流程可直接预览、上传的 File。
 * 读取沿用登录态、统一超时与错误 envelope；图片类型和 10 MiB 体积上限在这里先做一次本地校验。
 */
export const loadChatLibraryImage = async (asset: ChatLibraryImageAsset, options?: RequestControlOptions): Promise<File> => {
  // blob 是受登录保护的图片二进制内容，其类型来自服务端 Content-Type。
  const blob = await getBlob(imageLibraryContentURL(asset.id), { timeoutMs: 60_000, ...options });
  if (!blob.type.startsWith('image/')) throw new Error('资源中心图片内容不是图片');
  if (blob.size === 0) throw new Error('资源中心图片内容为空');
  if (blob.size > chatImageMaxBytes) throw new Error('资源中心图片超过 10 MiB，无法发送');
  // extension 是上传文件名后缀；未知图片类型按 png 兜底，真实 MIME 类型仍由 blob 提供。
  const extension = chatImageExtensionByMimeType[blob.type] || 'png';
  return new File([blob], `${asset.name || `image-${asset.id}`}.${extension}`, { type: blob.type });
};

/** ChatKeywordReplyTemplate 是快捷回复抽屉可一键导入的自动关键词回复文本模板。 */
export type ChatKeywordReplyTemplate = {
  /** id 是关键词规则稳定标识，用作列表键值。 */
  id: string;
  /** keyword 是触发该回复的首个表达式，仅用于向用户说明来源。 */
  keyword: string;
  /** content 是可写入账号快捷回复的文本正文；图片型关键词回复不参与导入。 */
  content: string;
};

/**
 * getChatKeywordReplies 读取当前账号的自动关键词回复，只保留可导入为文本快捷回复的模板。
 * 图片型回复和空正文会被跳过，相同正文按首次出现去重，避免抽屉里出现重复话术。
 */
export const getChatKeywordReplies = async (accountId: string, options?: RequestControlOptions): Promise<ChatKeywordReplyTemplate[]> => {
  // response 保存契约客户端返回的关键词规则集合，历史包裹响应按空集合降级。
  const response = await runContractRequest(/* signal 是本次关键词回复读取的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/reply-rules/{cid}/typed', {
    params: { path: { cid: accountId } },
    signal,
  }), options);
  if (!Array.isArray(response)) return [];
  // templates 保存按正文去重后的可导入模板。
  const templates: ChatKeywordReplyTemplate[] = [];
  for (const /* row 是当前待归一化的关键词规则 DTO。 */ row of response) {
    // content 是当前规则去除首尾空白后的回复正文。
    const content = typeof row.reply === 'string' ? row.reply.trim() : '';
    if (row.type === 'image' || !content) continue;
    if (templates.some(/* existing 是已收集的同正文模板。 */ existing => existing.content === content)) continue;
    templates.push({ id: String(row.id), keyword: (row.expressions && row.expressions[0]) || row.keyword || '', content });
  }
  return templates;
};
