// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { ImageLibraryContext } from '../../../../shared/components/ImageLibraryContext';
import { getItemGreetings, getItems, saveItemGreeting } from '../api';
import type { ItemGreetingRule } from '../api';
import ItemGreetingPanel from './ItemGreetingPanel';

vi.mock('../api', /* 首次回复使用真实 Hook，网络调用全部替换为本地可控响应。 */ () => ({
  getItemGreetings: vi.fn(), getItems: vi.fn(), saveItemGreeting: vi.fn(),
  clearItemGreetingRecords: vi.fn(), deleteItemGreeting: vi.fn(),
}));

/** originalRule 是历史 URL 规则，测试切换到资源图片后不携带旧 URL。 */
const originalRule: ItemGreetingRule = { id: 1, cookie_id: 'a1', name: '欢迎语', enabled: true, reply_content: '', reply_image_url: 'https://example.test/old.png', reply_image_path: '', item_ids: ['i1'] };

beforeEach(/* 每例重置列表与保存替身，避免测试间共享草稿。 */ () => {
  vi.clearAllMocks();
  vi.mocked(getItemGreetings).mockResolvedValue([originalRule]);
  vi.mocked(getItems).mockResolvedValue([]);
  vi.mocked(saveItemGreeting).mockResolvedValue(originalRule);
});
afterEach(/* 清理弹窗 Portal 和计时器所属组件。 */ () => cleanup());

/** renderPanel 提供已上传的消息图片选项，所有选择行为仍走真实控件和 Hook。 */
const renderPanel = () => render(<ImageLibraryContext.Provider value={[{ id: 7, name: '使用说明', reference: '_library/guide.png' }]}><ItemGreetingPanel accountID="a1" /></ImageLibraryContext.Provider>);

test('首次回复从已有消息图片选择并保存，切换来源清空隐藏字段', /* 验证缩略图、无上传/手填入口和两种来源互斥。 */ async () => {
  // view 用于确认弹窗中没有文件上传控件。
  const view = renderPanel();
  await screen.findByText('欢迎语');
  fireEvent.click(screen.getByTitle('编辑'));
  expect((screen.getByLabelText('回复图片 URL（可选）') as HTMLInputElement).value).toBe(originalRule.reply_image_url);
  fireEvent.change(screen.getByLabelText('图片来源（可选）'), { target: { value: 'local' } });
  fireEvent.change(screen.getByRole('combobox', { name: '资源中心图片' }), { target: { value: '_library/guide.png' } });
  expect(screen.getByRole('img', { name: '使用说明 缩略图' }).getAttribute('src')).toBe('/api/v1/image-library/7/content');
  expect(screen.queryByLabelText('本地图片相对路径')).toBeNull();
  expect(view.baseElement.querySelector('input[type=file]')).toBeNull();
  fireEvent.change(screen.getByLabelText('图片来源（可选）'), { target: { value: 'url' } });
  expect((screen.getByLabelText('回复图片 URL（可选）') as HTMLInputElement).value).toBe('');
  fireEvent.change(screen.getByLabelText('回复图片 URL（可选）'), { target: { value: 'https://example.test/new.png' } });
  fireEvent.change(screen.getByLabelText('图片来源（可选）'), { target: { value: 'local' } });
  expect((screen.getByRole('combobox', { name: '资源中心图片' }) as HTMLSelectElement).value).toBe('');
  fireEvent.change(screen.getByRole('combobox', { name: '资源中心图片' }), { target: { value: '_library/guide.png' } });
  fireEvent.click(screen.getByRole('button', { name: '保存' }));
  await waitFor(/* 保存请求必须仅携带本地图片来源。 */ () => expect(saveItemGreeting).toHaveBeenCalledWith(expect.objectContaining({ reply_content: '', reply_image_url: '', reply_image_path: '_library/guide.png' })));
  await waitFor(/* 保存成功关闭编辑器。 */ () => expect(screen.queryByText('编辑首次回复')).toBeNull());
});

test('已保存资源图片回显，取消编辑不会提交且重新打开恢复原值', /* 覆盖本地图回读与取消草稿。 */ async () => {
  vi.mocked(getItemGreetings).mockResolvedValue([{ ...originalRule, reply_image_url: '', reply_image_path: '_library/guide.png' }]);
  renderPanel();
  await screen.findByText('欢迎语');
  fireEvent.click(screen.getByTitle('编辑'));
  expect((screen.getByLabelText('图片来源（可选）') as HTMLSelectElement).value).toBe('local');
  expect((screen.getByRole('combobox', { name: '资源中心图片' }) as HTMLSelectElement).value).toBe('_library/guide.png');
  fireEvent.change(screen.getByRole('combobox', { name: '资源中心图片' }), { target: { value: '' } });
  fireEvent.click(screen.getByRole('button', { name: '取消' }));
  expect(saveItemGreeting).not.toHaveBeenCalled();
  fireEvent.click(screen.getByTitle('编辑'));
  expect((screen.getByRole('combobox', { name: '资源中心图片' }) as HTMLSelectElement).value).toBe('_library/guide.png');
});
