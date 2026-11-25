// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,within } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import ChatMetadataFeature from './ChatMetadataFeature';
import { useChatMetadata } from '../metadata';
import type { ChatMetadataState } from '../metadata';
import type { ChatQuickReply,ChatSession } from '../models';
import type { ChatKeywordReplyTemplate } from '../api';

vi.mock('../metadata', /* chatMetadataMockFactory 为抽屉组件交互测试提供受控的元数据 Hook 替身。 */ () => ({ useChatMetadata: vi.fn() }));


afterEach(/* 当前回调卸载每个抽屉场景的 DOM，避免跨用例命中重复按钮。 */ () => cleanup());
// useChatMetadataMock 是可为每个测试设置返回状态的元数据 Hook 替身。
const useChatMetadataMock = vi.mocked(useChatMetadata);
// quickReplyFixture 是用于验证操作顺序和关闭行为的账号级快捷回复。
const quickReplyFixture: ChatQuickReply = { id: 1, account_id: 'account-1', content: '测试话术', created_at: 1 };
// sessionFixture 是使快捷回复发送按钮可用的当前聊天会话。
const sessionFixture: ChatSession = { account_id: 'account-1', chat_id: 'chat-1', peer_user_id: 'buyer-1', peer_name: '买家', account_role: 'seller', buyer_user_id: 'buyer-1', seller_user_id: 'self-1', last_message: '', last_message_at: 1, unread_count: 0 };
// keywordReplyFixture 是当前账号可一键导入的自动关键词回复文本模板。
const keywordReplyFixture: ChatKeywordReplyTemplate = { id: 'k1', keyword: '现货', content: '现货可拍，当天发出' };
/** metadataFixture 构造抽屉组件交互所需的完整元数据状态，并允许测试观测复制、导入动作和覆盖单项状态。 */
const metadataFixture = (copyQuickReply: ChatMetadataState['copyQuickReply'], overrides: Partial<ChatMetadataState> = {}): ChatMetadataState => ({
  quickReplies: [quickReplyFixture], quickReplyDraft: '', setQuickReplyDraft: vi.fn(), quickReplyBusy: false,
  pendingDeleteQuickReply: null, requestQuickReplyDelete: vi.fn(), cancelQuickReplyDelete: vi.fn(), confirmQuickReplyDelete: vi.fn().mockResolvedValue(undefined),
  addQuickReply: vi.fn().mockResolvedValue(undefined), copyQuickReply, copiedQuickReplyID: null, buyerNote: null, noteLoading: false,
  noteDialogOpen: false, noteEditing: false, noteDraft: '', setNoteDraft: vi.fn(), noteSaving: false, openNoteDialog: vi.fn(),
  closeNoteDialog: vi.fn(), beginNoteEditing: vi.fn(), saveBuyerNote: vi.fn().mockResolvedValue(undefined), metadataError: '', clearMetadataError: vi.fn(),
  keywordReplies: [keywordReplyFixture], keywordRepliesLoading: false, keywordRepliesLoaded: true, loadKeywordReplies: vi.fn().mockResolvedValue(undefined),
  importingKeywordReplyID: null, importKeywordReply: vi.fn().mockResolvedValue(undefined), ...overrides,
});

describe('ChatMetadataFeature', /* 当前回调验证快捷回复抽屉的操作排序和关闭交互。 */ () => {
  beforeEach(/* 当前回调在每个抽屉交互场景前重置 Hook 替身调用记录。 */ () => {
    vi.clearAllMocks();
  });

  test('删除、复制、发送排序正确，复制发送和外部点击均收起抽屉', /* 当前回调验证快捷回复操作的高频关闭流程。 */ async () => {
    // copyQuickReply 保存可观测的复制动作替身。
    const copyQuickReply = vi.fn().mockResolvedValue(undefined);
    // closeQuickReplyPanel 保存抽屉关闭动作替身。
    const closeQuickReplyPanel = vi.fn();
    // sendQuickReply 保存当前会话模板发送动作替身。
    const sendQuickReply = vi.fn().mockResolvedValue(undefined);
    useChatMetadataMock.mockReturnValue(metadataFixture(copyQuickReply));
    render(<><ChatMetadataFeature activeAccountID="account-1" selectedSession={sessionFixture} quickReplyPanelOpen closeQuickReplyPanel={closeQuickReplyPanel} sendQuickReply={sendQuickReply} sending={false} accountOnline /><button type="button">抽屉外区域</button></>);
    // replyCard 是包含当前快捷回复操作按钮的卡片元素。
    const replyCard = screen.getByText('测试话术').closest('article');
    if (replyCard == null) throw new Error('未找到快捷回复卡片');
    // actionNames 保存卡片内按钮按视觉顺序的可访问名称。
    const actionNames = within(replyCard).getAllByRole('button').map(/* actionButton 保存当前待检查的快捷回复操作按钮。 */ actionButton => actionButton.getAttribute('aria-label') || actionButton.textContent);
    expect(actionNames).toEqual(['删除快捷回复', '复制', '发送']);
    fireEvent.click(within(replyCard).getByRole('button', { name: '复制' }));
    expect(copyQuickReply).toHaveBeenCalledWith(quickReplyFixture);
    expect(closeQuickReplyPanel).toHaveBeenCalledTimes(1);
    fireEvent.click(within(replyCard).getByRole('button', { name: '发送' }));
    expect(sendQuickReply).toHaveBeenCalledWith('测试话术');
    expect(closeQuickReplyPanel).toHaveBeenCalledTimes(2);
    fireEvent.pointerDown(screen.getByRole('button', { name: '抽屉外区域' }));
    expect(closeQuickReplyPanel).toHaveBeenCalledTimes(3);
  });

	test('仅卖家侧已确认买家显示备注入口', /* 当前回调验证买家侧会话不会把对端卖家误当作买家记录备注。 */ () => {
		// copyQuickReply 是本场景不触发的复制替身。
		const copyQuickReply = vi.fn().mockResolvedValue(undefined);
		useChatMetadataMock.mockReturnValue(metadataFixture(copyQuickReply));
		// view 是可切换会话角色重新渲染的组件实例。
		const view = render(<ChatMetadataFeature activeAccountID="account-1" selectedSession={sessionFixture} quickReplyPanelOpen={false} closeQuickReplyPanel={vi.fn()} sendQuickReply={vi.fn()} sending={false} accountOnline />);
		expect(within(view.container).getByRole('button', { name: '添加用户备注' })).not.toBeNull();
		// buyerSideSession 把当前账号设为买家，对端因此是卖家。
		const buyerSideSession: ChatSession = { ...sessionFixture, account_role: 'buyer', buyer_user_id: 'self-1', seller_user_id: 'seller-peer', peer_user_id: 'seller-peer', peer_name: '卖家' };
		view.rerender(<ChatMetadataFeature activeAccountID="account-1" selectedSession={buyerSideSession} quickReplyPanelOpen={false} closeQuickReplyPanel={vi.fn()} sendQuickReply={vi.fn()} sending={false} accountOnline />);
		expect(within(view.container).queryByRole('button', { name: '添加用户备注' })).toBeNull();
	});

	test('展开关键词回复区块后可按模板加入快捷回复', /* 当前回调验证导入区块的按需读取与单条添加动作。 */ () => {
		// loadKeywordReplies 是导入区块首次展开时的读取动作替身。
		const loadKeywordReplies = vi.fn().mockResolvedValue(undefined);
		// importKeywordReply 是单条模板加入快捷回复的动作替身。
		const importKeywordReply = vi.fn().mockResolvedValue(undefined);
		useChatMetadataMock.mockReturnValue(metadataFixture(vi.fn().mockResolvedValue(undefined), { loadKeywordReplies, importKeywordReply }));
		render(<ChatMetadataFeature activeAccountID="account-1" selectedSession={sessionFixture} quickReplyPanelOpen closeQuickReplyPanel={vi.fn()} sendQuickReply={vi.fn()} sending={false} accountOnline />);
		fireEvent.click(screen.getByRole('button', { name: '从关键词回复添加' }));
		expect(loadKeywordReplies).toHaveBeenCalledTimes(1);
		expect(screen.getByText('现货可拍，当天发出')).not.toBeNull();
		fireEvent.click(screen.getByRole('button', { name: '添加' }));
		expect(importKeywordReply).toHaveBeenCalledWith(keywordReplyFixture);
	});

	test('快捷回复达到上限时导入按钮不可用', /* 当前回调验证 50 条上限对导入动作的约束。 */ () => {
		// loadKeywordReplies 是本场景不参与断言的读取动作替身。
		const loadKeywordReplies = vi.fn().mockResolvedValue(undefined);
		useChatMetadataMock.mockReturnValue(metadataFixture(vi.fn().mockResolvedValue(undefined), {
			loadKeywordReplies,
			quickReplies: Array.from({ length: 50 }, /* 当前回调生成占位快捷回复。 */ (_value, index) => ({ id: index + 1, account_id: 'account-1', content: `话术${index + 1}`, created_at: index })),
		}));
		render(<ChatMetadataFeature activeAccountID="account-1" selectedSession={sessionFixture} quickReplyPanelOpen closeQuickReplyPanel={vi.fn()} sendQuickReply={vi.fn()} sending={false} accountOnline />);
		fireEvent.click(screen.getByRole('button', { name: '从关键词回复添加' }));
		// importButton 是达到上限后的导入按钮。
		const importButton = screen.getByRole('button', { name: '添加' }) as HTMLButtonElement;
		expect(importButton.disabled).toBe(true);
	});
});
