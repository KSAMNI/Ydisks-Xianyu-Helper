import { useCallback, useEffect, useRef, useState } from 'react';
import { createChatQuickReply, deleteChatQuickReply, getChatBuyerNote, getChatKeywordReplies, getChatQuickReplies, saveChatBuyerNote } from './api';
import type { ChatKeywordReplyTemplate } from './api';
import type { ChatBuyerNote, ChatQuickReply, ChatSession } from './models';
import { isChatAbortError } from './state';

/** ChatMetadataState 描述聊天页面中快捷回复与买家备注的短暂 UI 状态和交互动作。 */
export type ChatMetadataState = {
  /** quickReplies 保存当前账号可用的快捷回复。 */
  quickReplies: ChatQuickReply[];
  /** quickReplyDraft 保存底部新增快捷回复输入框的表单文本。 */
  quickReplyDraft: string;
  /** setQuickReplyDraft 更新新增快捷回复表单文本。 */
  setQuickReplyDraft: (content: string) => void;
  /** quickReplyBusy 表示新增或删除快捷回复请求正在进行。 */
  quickReplyBusy: boolean;
  /** pendingDeleteQuickReply 保存等待用户确认删除的快捷回复；空值表示未打开确认框。 */
  pendingDeleteQuickReply: ChatQuickReply | null;
  /** requestQuickReplyDelete 打开指定快捷回复的删除确认框。 */
  requestQuickReplyDelete: (reply: ChatQuickReply) => void;
  /** cancelQuickReplyDelete 关闭删除确认框而不改变数据。 */
  cancelQuickReplyDelete: () => void;
  /** confirmQuickReplyDelete 删除已确认的快捷回复。 */
  confirmQuickReplyDelete: () => Promise<void>;
  /** addQuickReply 保存底部输入框中的快捷回复。 */
  addQuickReply: () => Promise<void>;
  /** copyQuickReply 将一条快捷回复写入系统剪贴板。 */
  copyQuickReply: (reply: ChatQuickReply) => Promise<void>;
  /** copiedQuickReplyID 保存最近复制成功的快捷回复标识，用于显示短暂反馈。 */
  copiedQuickReplyID: number | null;
  /** buyerNote 保存当前聊天买家的完整备注；无选中会话时为空。 */
  buyerNote: ChatBuyerNote | null;
  /** noteLoading 表示当前会话买家备注正在查询。 */
  noteLoading: boolean;
  /** noteDialogOpen 控制买家备注弹窗的可见性。 */
  noteDialogOpen: boolean;
  /** noteEditing 控制备注弹窗处于只读查看还是编辑状态。 */
  noteEditing: boolean;
  /** noteDraft 保存备注弹窗编辑态的表单文本。 */
  noteDraft: string;
  /** setNoteDraft 更新备注编辑框的表单文本。 */
  setNoteDraft: (content: string) => void;
  /** noteSaving 表示备注保存请求正在进行。 */
  noteSaving: boolean;
  /** openNoteDialog 打开完整备注弹窗并默认显示只读内容。 */
  openNoteDialog: () => void;
  /** closeNoteDialog 关闭备注弹窗并放弃未保存表单变更。 */
  closeNoteDialog: () => void;
  /** beginNoteEditing 将备注弹窗切换为编辑态。 */
  beginNoteEditing: () => void;
  /** saveBuyerNote 保存当前买家备注。 */
  saveBuyerNote: () => Promise<void>;
  /** metadataError 保存快捷回复或买家备注操作的最近用户可见错误。 */
  metadataError: string;
  /** clearMetadataError 清除当前元数据错误提示。 */
  clearMetadataError: () => void;
  /** keywordReplies 保存当前账号可导入为快捷回复的自动关键词回复文本模板。 */
  keywordReplies: ChatKeywordReplyTemplate[];
  /** keywordRepliesLoading 表示当前账号的关键词回复模板正在读取。 */
  keywordRepliesLoading: boolean;
  /** keywordRepliesLoaded 表示当前账号的模板已成功读取一次，避免重复请求。 */
  keywordRepliesLoaded: boolean;
  /** loadKeywordReplies 读取当前账号的关键词回复模板；已加载或读取中时不重复请求。 */
  loadKeywordReplies: () => Promise<void>;
  /** importingKeywordReplyID 保存正在导入的模板标识，空值表示没有导入请求。 */
  importingKeywordReplyID: string | null;
  /** importKeywordReply 把选中的关键词回复正文新增为当前账号快捷回复。 */
  importKeywordReply: (template: ChatKeywordReplyTemplate) => Promise<void>;
};

/** useChatMetadata 管理账号级快捷回复及当前会话买家备注，并隔离账号/会话切换后的晚到响应。 */
export const useChatMetadata = (activeAccountID: string, selectedSession: ChatSession | undefined): ChatMetadataState => {
  /** quickReplies 保存当前账号可用的快捷回复；账号切换时以最新请求响应替换。 */
  const [quickReplies, setQuickReplies] = useState<ChatQuickReply[]>([]);
  /** quickReplyDraft 保存新增快捷回复的小型输入框文本。 */
  const [quickReplyDraft, setQuickReplyDraft] = useState('');
  /** quickReplyBusy 防止新增或删除过程中重复提交快捷回复变更。 */
  const [quickReplyBusy, setQuickReplyBusy] = useState(false);
  /** pendingDeleteQuickReply 保存等待确认删除的快捷回复，避免误触即删除。 */
  const [pendingDeleteQuickReply, setPendingDeleteQuickReply] = useState<ChatQuickReply | null>(null);
  /** copiedQuickReplyID 保存最近复制成功的回复标识，用于按钮内的短暂成功反馈。 */
  const [copiedQuickReplyID, setCopiedQuickReplyID] = useState<number | null>(null);
  /** buyerNote 保存当前账号和买家组合对应的完整备注。 */
  const [buyerNote, setBuyerNote] = useState<ChatBuyerNote | null>(null);
  /** noteLoading 表示当前会话的买家备注仍在读取。 */
  const [noteLoading, setNoteLoading] = useState(false);
  /** noteDialogOpen 控制完整买家备注弹窗显示。 */
  const [noteDialogOpen, setNoteDialogOpen] = useState(false);
  /** noteEditing 控制备注弹窗是否显示可写表单。 */
  const [noteEditing, setNoteEditing] = useState(false);
  /** noteDraft 保存备注编辑中的表单正文，与服务端数据分离。 */
  const [noteDraft, setNoteDraft] = useState('');
  /** noteSaving 防止备注保存期间发生重复提交。 */
  const [noteSaving, setNoteSaving] = useState(false);
  /** metadataError 保存元数据请求失败的用户可见说明。 */
  const [metadataError, setMetadataError] = useState('');
  /** keywordReplies 保存当前账号可导入的自动关键词回复模板，账号切换时清空。 */
  const [keywordReplies, setKeywordReplies] = useState<ChatKeywordReplyTemplate[]>([]);
  /** keywordRepliesLoading 表示关键词回复模板请求仍在进行。 */
  const [keywordRepliesLoading, setKeywordRepliesLoading] = useState(false);
  /** keywordRepliesLoaded 表示当前账号的模板已成功读取，用于避免重复请求。 */
  const [keywordRepliesLoaded, setKeywordRepliesLoaded] = useState(false);
  /** importingKeywordReplyID 保存正在导入的模板标识，防止重复点击产生多条快捷回复。 */
  const [importingKeywordReplyID, setImportingKeywordReplyID] = useState<string | null>(null);
  /** quickReplyControllerRef 保存当前账号快捷回复读取请求的取消控制器。 */
  const quickReplyControllerRef = useRef<AbortController | null>(null);
  /** quickReplySequenceRef 为账号快捷回复查询分配单调递增代次，阻止晚到响应覆盖新账号。 */
  const quickReplySequenceRef = useRef(0);
  /** noteControllerRef 保存当前买家备注读取请求的取消控制器。 */
  const noteControllerRef = useRef<AbortController | null>(null);
  /** noteSequenceRef 为买家备注查询分配单调递增代次，阻止旧会话覆盖新会话。 */
  const noteSequenceRef = useRef(0);
  /** copyTimerRef 保存复制反馈自动关闭计时器，卸载时由当前 Hook 清理。 */
  const copyTimerRef = useRef<number | null>(null);
  /** keywordControllerRef 保存当前关键词回复模板读取请求的取消控制器。 */
  const keywordControllerRef = useRef<AbortController | null>(null);
  /** keywordSequenceRef 为模板读取分配单调递增代次，阻止账号切换后的晚到响应。 */
  const keywordSequenceRef = useRef(0);

  useEffect(/* 当前副作用在账号切换时读取账号级快捷回复，并在 cleanup 中取消旧请求。 */ () => {
    quickReplyControllerRef.current?.abort();
    // sequence 保存本次账号快捷回复请求代次。
    const sequence = ++quickReplySequenceRef.current;
    if (!activeAccountID) {
      setQuickReplies([]);
      setQuickReplyDraft('');
      return undefined;
    }
    setQuickReplies([]);
    setQuickReplyDraft('');
    // controller 保存本次快捷回复请求的取消控制器。
    const controller = new AbortController();
    quickReplyControllerRef.current = controller;
    void getChatQuickReplies(activeAccountID, { signal: controller.signal }).then(/* replies 保存当前账号查询返回的快捷回复列表。 */ replies => {
      if (!controller.signal.aborted && sequence === quickReplySequenceRef.current) setQuickReplies(replies);
    }).catch(/* loadError 保存快捷回复查询失败原因；中止请求不显示错误。 */ loadError => {
      if (!controller.signal.aborted && sequence === quickReplySequenceRef.current && !isChatAbortError(loadError)) setMetadataError(loadError instanceof Error ? loadError.message : '读取快捷回复失败');
    });
    return /* 当前 cleanup 取消账号切换后不再有效的快捷回复请求。 */ () => controller.abort();
  }, [activeAccountID]);

  useEffect(/* 当前副作用在账号或买家切换时读取备注，并丢弃已失效会话的响应。 */ () => {
    noteControllerRef.current?.abort();
    // sequence 保存本次买家备注请求代次。
    const sequence = ++noteSequenceRef.current;
    // buyerID 保存当前选中会话的稳定买家标识；没有会话时不发起请求。
    const buyerID = selectedSession?.account_role === 'seller' ? (selectedSession.buyer_user_id || '') : '';
    if (!activeAccountID || !buyerID) {
      setBuyerNote(null);
      setNoteLoading(false);
      setNoteDialogOpen(false);
      setNoteEditing(false);
      return undefined;
    }
    // controller 保存本次买家备注请求的取消控制器。
    const controller = new AbortController();
    noteControllerRef.current = controller;
    setNoteLoading(true);
    void getChatBuyerNote(activeAccountID, buyerID, { signal: controller.signal }).then(/* note 保存当前账号与买家组合的完整备注。 */ note => {
      if (!controller.signal.aborted && sequence === noteSequenceRef.current) {
        setBuyerNote(note);
        setNoteDraft(note.content);
      }
    }).catch(/* loadError 保存买家备注读取失败原因；中止请求不显示错误。 */ loadError => {
      if (!controller.signal.aborted && sequence === noteSequenceRef.current && !isChatAbortError(loadError)) setMetadataError(loadError instanceof Error ? loadError.message : '读取买家备注失败');
    }).finally(/* 当前回调在仍为最新请求时结束备注加载状态。 */ () => {
      if (!controller.signal.aborted && sequence === noteSequenceRef.current) setNoteLoading(false);
    });
    return /* 当前 cleanup 取消会话切换后不再有效的买家备注请求。 */ () => controller.abort();
  }, [activeAccountID, selectedSession?.account_role, selectedSession?.buyer_user_id]);

  useEffect(/* 当前副作用在 Hook 卸载时清理复制反馈计时器并取消模板读取，避免卸载后写入 React 状态。 */ () => {
    /** cleanupCopyTimer 清理仍在运行的复制反馈计时器。 */
    const cleanupCopyTimer = (): void => {
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
    };
    return /* 当前 cleanup 同时取消未完成的关键词回复模板请求。 */ () => {
      cleanupCopyTimer();
      keywordSequenceRef.current += 1;
      keywordControllerRef.current?.abort();
    };
  }, []);

  useEffect(/* 当前副作用在账号切换时清空旧账号的关键词回复模板并取消在途读取。 */ () => {
    keywordSequenceRef.current += 1;
    keywordControllerRef.current?.abort();
    setKeywordReplies([]);
    setKeywordRepliesLoading(false);
    setKeywordRepliesLoaded(false);
    setImportingKeywordReplyID(null);
  }, [activeAccountID]);

  /** createQuickReplyFromContent 把指定正文新增为当前账号快捷回复，并在成功后插入列表头部。 */
  const createQuickReplyFromContent = useCallback(/* 当前回调统一处理手输和导入两条新增快捷回复路径。 */ async (content: string): Promise<boolean> => {
    // trimmed 保存去除首尾空白后的待创建文本，内部换行保持原样。
    const trimmed = content.trim();
    if (!activeAccountID || !trimmed || quickReplyBusy) return false;
    setQuickReplyBusy(true);
    setMetadataError('');
    try {
      // reply 保存服务端创建并返回的快捷回复。
      const reply = await createChatQuickReply(activeAccountID, trimmed);
      setQuickReplies(/* 当前回调将新建记录插入按最新优先排列的列表。 */ current => [reply, ...current]);
      return true;
    } catch (/* createError 保存创建快捷回复失败原因。 */ createError) {
      if (!isChatAbortError(createError)) setMetadataError(createError instanceof Error ? createError.message : '添加快捷回复失败');
      return false;
    } finally {
      setQuickReplyBusy(false);
    }
  }, [activeAccountID, quickReplyBusy]);

  /** 新增当前账号的快捷回复，并在服务端成功后清空底部输入框。 */
  const addQuickReply = useCallback(/* 当前回调提交底部快捷回复表单。 */ async (): Promise<void> => {
    if (await createQuickReplyFromContent(quickReplyDraft)) setQuickReplyDraft('');
  }, [createQuickReplyFromContent, quickReplyDraft]);

  /** loadKeywordReplies 读取当前账号的自动关键词回复文本模板，供抽屉一键导入。 */
  const loadKeywordReplies = useCallback(/* 当前回调在用户展开导入区块时读取关键词回复模板。 */ async (): Promise<void> => {
    if (!activeAccountID || keywordRepliesLoading || keywordRepliesLoaded) return;
    // sequence 标识本次模板读取，账号切换后旧响应必须丢弃。
    const sequence = ++keywordSequenceRef.current;
    keywordControllerRef.current?.abort();
    // controller 保存本次模板读取的取消边界。
    const controller = new AbortController();
    keywordControllerRef.current = controller;
    setKeywordRepliesLoading(true);
    setMetadataError('');
    try {
      // templates 保存服务端归一后的可导入文本模板。
      const templates = await getChatKeywordReplies(activeAccountID, { signal: controller.signal });
      if (sequence !== keywordSequenceRef.current || controller.signal.aborted) return;
      setKeywordReplies(templates);
      setKeywordRepliesLoaded(true);
    } catch (/* loadError 保存关键词回复模板读取失败原因；中止请求不显示错误。 */ loadError) {
      if (sequence === keywordSequenceRef.current && !isChatAbortError(loadError)) setMetadataError(loadError instanceof Error ? loadError.message : '读取关键词回复失败');
    } finally {
      if (sequence === keywordSequenceRef.current) setKeywordRepliesLoading(false);
    }
  }, [activeAccountID, keywordRepliesLoaded, keywordRepliesLoading]);

  /** importKeywordReply 把选中的关键词回复正文新增为当前账号快捷回复。 */
  const importKeywordReply = useCallback(/* 当前回调响应导入区块中的单条添加按钮。 */ async (template: ChatKeywordReplyTemplate): Promise<void> => {
    if (importingKeywordReplyID) return;
    setImportingKeywordReplyID(template.id);
    try {
      await createQuickReplyFromContent(template.content);
    } finally {
      setImportingKeywordReplyID(null);
    }
  }, [createQuickReplyFromContent, importingKeywordReplyID]);

  /** requestQuickReplyDelete 打开快捷回复删除确认框。 */
  const requestQuickReplyDelete = useCallback(/* 当前回调记录用户请求删除的快捷回复。 */ (reply: ChatQuickReply): void => {
    setPendingDeleteQuickReply(reply);
  }, []);

  /** cancelQuickReplyDelete 关闭删除确认框，不改变快捷回复列表。 */
  const cancelQuickReplyDelete = useCallback(/* 当前回调放弃当前待确认的快捷回复删除动作。 */ (): void => {
    setPendingDeleteQuickReply(null);
  }, []);

  /** confirmQuickReplyDelete 删除用户已确认的快捷回复。 */
  const confirmQuickReplyDelete = useCallback(/* 当前回调提交删除确认框中的快捷回复删除操作。 */ async (): Promise<void> => {
    if (!activeAccountID || !pendingDeleteQuickReply || quickReplyBusy) return;
    // quickReplyID 保存确认删除的稳定快捷回复标识，避免异步请求读取会变的 state。
    const quickReplyID = pendingDeleteQuickReply.id;
    setQuickReplyBusy(true);
    setMetadataError('');
    try {
      await deleteChatQuickReply(activeAccountID, quickReplyID);
      setQuickReplies(/* 当前回调只移除服务端已成功删除的快捷回复。 */ current => current.filter(/* reply 保存当前参与删除匹配的快捷回复。 */ reply => reply.id !== quickReplyID));
      setPendingDeleteQuickReply(null);
    } catch (/* deleteError 保存删除快捷回复失败原因。 */ deleteError) {
      if (!isChatAbortError(deleteError)) setMetadataError(deleteError instanceof Error ? deleteError.message : '删除快捷回复失败');
    } finally {
      setQuickReplyBusy(false);
    }
  }, [activeAccountID, pendingDeleteQuickReply, quickReplyBusy]);

  /** copyQuickReply 将快捷回复正文复制到系统剪贴板，并给出短暂成功反馈。 */
  const copyQuickReply = useCallback(/* 当前回调响应快捷回复复制按钮的用户操作。 */ async (reply: ChatQuickReply): Promise<void> => {
    if (!navigator.clipboard?.writeText) {
      setMetadataError('当前浏览器不支持复制到剪贴板');
      return;
    }
    setMetadataError('');
    try {
      await navigator.clipboard.writeText(reply.content);
      setCopiedQuickReplyID(reply.id);
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
      copyTimerRef.current = window.setTimeout(/* 当前回调在短暂反馈结束后清除复制成功状态。 */ () => setCopiedQuickReplyID(null), 1600);
    } catch (/* copyError 保存系统剪贴板拒绝写入时的失败原因。 */ copyError) {
      setMetadataError(copyError instanceof Error ? copyError.message : '复制快捷回复失败');
    }
  }, []);

  /** openNoteDialog 打开当前买家的完整备注，并默认进入只读查看状态。 */
  const openNoteDialog = useCallback(/* 当前回调响应聊天标题中的备注摘要点击。 */ (): void => {
    setNoteDraft(buyerNote?.content || '');
    setNoteEditing(false);
    setNoteDialogOpen(true);
  }, [buyerNote?.content]);

  /** closeNoteDialog 关闭备注弹窗并恢复当前已保存备注，放弃未保存草稿。 */
  const closeNoteDialog = useCallback(/* 当前回调响应备注弹窗的关闭操作。 */ (): void => {
    setNoteDraft(buyerNote?.content || '');
    setNoteEditing(false);
    setNoteDialogOpen(false);
  }, [buyerNote?.content]);

  /** beginNoteEditing 将当前备注弹窗切换为可编辑状态。 */
  const beginNoteEditing = useCallback(/* 当前回调响应备注弹窗中的编辑按钮。 */ (): void => {
    setNoteDraft(buyerNote?.content || '');
    setNoteEditing(true);
  }, [buyerNote?.content]);

  /** saveBuyerNote 保存当前会话买家的备注，并在成功后回到只读查看状态。 */
  const saveBuyerNote = useCallback(/* 当前回调提交买家备注编辑表单。 */ async (): Promise<void> => {
    // buyerID 保存当前会话的稳定买家标识；会话切换后不允许把旧备注写到新买家。
    const buyerID = selectedSession?.account_role === 'seller' ? (selectedSession.buyer_user_id || '') : '';
    if (!activeAccountID || !buyerID || noteSaving) return;
    setNoteSaving(true);
    setMetadataError('');
    try {
      // note 保存服务端标准化并持久化后的买家备注。
      const note = await saveChatBuyerNote(activeAccountID, buyerID, noteDraft);
      setBuyerNote(note);
      setNoteDraft(note.content);
      setNoteEditing(false);
    } catch (/* saveError 保存备注保存失败原因。 */ saveError) {
      if (!isChatAbortError(saveError)) setMetadataError(saveError instanceof Error ? saveError.message : '保存买家备注失败');
    } finally {
      setNoteSaving(false);
    }
  }, [activeAccountID, noteDraft, noteSaving, selectedSession?.account_role, selectedSession?.buyer_user_id]);

  /** clearMetadataError 清除快捷回复和备注区域最近展示的错误。 */
  const clearMetadataError = useCallback(/* 当前回调在用户继续编辑或关闭错误提示时清空旧错误。 */ (): void => {
    setMetadataError('');
  }, []);

  return {
    quickReplies, quickReplyDraft, setQuickReplyDraft, quickReplyBusy,
    pendingDeleteQuickReply, requestQuickReplyDelete, cancelQuickReplyDelete, confirmQuickReplyDelete, addQuickReply,
    copyQuickReply, copiedQuickReplyID, buyerNote, noteLoading, noteDialogOpen, noteEditing, noteDraft, setNoteDraft,
    noteSaving, openNoteDialog, closeNoteDialog, beginNoteEditing, saveBuyerNote, metadataError, clearMetadataError,
    keywordReplies, keywordRepliesLoading, keywordRepliesLoaded, loadKeywordReplies, importingKeywordReplyID, importKeywordReply,
  };
};
