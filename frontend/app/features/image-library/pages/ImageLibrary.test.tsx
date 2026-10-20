// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { deleteImageAsset, listImageAssets, renameImageAsset, uploadImageAsset } from '../api';
import type { ImageAsset } from '../models';
import ImageLibrary from './ImageLibrary';

vi.mock('../api', /* imageLibraryApiMockFactory 提供资源中心页面全部请求替身。 */ () => ({
  deleteImageAsset: vi.fn(),
  isImageLibraryConflict: /* 把带自有 status 字段的失败视为 409 冲突，供强制删除重试分支使用。 */ (error: unknown) => Object.prototype.hasOwnProperty.call(Object(error), 'status'),
  listImageAssets: vi.fn(),
  renameImageAsset: vi.fn(),
  uploadImageAsset: vi.fn(),
}));

/** listMock 是图片列表读取接口的可控替身。 */
const listMock = vi.mocked(listImageAssets);
/** uploadMock 是图片上传接口的可控替身。 */
const uploadMock = vi.mocked(uploadImageAsset);
/** renameMock 是图片改名接口的可控替身。 */
const renameMock = vi.mocked(renameImageAsset);
/** deleteMock 是图片删除接口的可控替身。 */
const deleteMock = vi.mocked(deleteImageAsset);

/** buildAsset 构造一张资源中心图片，未指定的字段使用稳定默认值。 */
const buildAsset = (overrides: Partial<ImageAsset> = {}): ImageAsset => ({
  id: 1,
  name: '主图',
  reference: '_library/main.png',
  mimeType: 'image/png',
  sizeBytes: 2048,
  width: 800,
  height: 600,
  usageCount: 0,
  createdAt: '2024-01-01T00:00:00Z',
  updatedAt: '2024-01-01T00:00:00Z',
  ...overrides,
});

/** primaryAsset 是未被任何配置引用的主图，删除时不需要二次确认。 */
const primaryAsset = buildAsset();
/** guideAsset 是仍被两处配置引用的教程图，删除前必须确认。 */
const guideAsset = buildAsset({ id: 2, name: '教程图', reference: '_library/guide.jpg', mimeType: 'image/jpeg', sizeBytes: 3 * 1024 * 1024, width: 1080, height: 1920, usageCount: 2 });

beforeEach(/* 当前回调重置资源中心接口替身和浏览器确认框。 */ () => {
  vi.clearAllMocks();
  listMock.mockResolvedValue([primaryAsset, guideAsset]);
  uploadMock.mockResolvedValue(primaryAsset);
  renameMock.mockResolvedValue(primaryAsset);
  deleteMock.mockResolvedValue(undefined);
  vi.spyOn(window, 'confirm').mockReturnValue(true);
});

afterEach(/* 当前回调清理资源中心页面 DOM 和全局替身。 */ () => {
  cleanup();
  vi.restoreAllMocks();
});

describe('资源中心页面', /* 当前回调覆盖列表、上传、改名、删除和竞态行为。 */ () => {
  test('列表渲染缩略图、尺寸、体积和引用次数', /* 当前回调确认网格展示服务端返回的全部摘要。 */ async () => {
    render(<ImageLibrary />);
    expect(await screen.findByText('主图')).toBeTruthy();
    expect(screen.getByText('教程图')).toBeTruthy();
    // thumbnail 是主图缩略图，地址必须走受登录保护的图片内容端点。
    const thumbnail = screen.getByRole('img', { name: '主图 缩略图' }) as HTMLImageElement;
    expect(thumbnail.getAttribute('src')).toBe('/api/v1/image-library/1/content');
    expect(thumbnail.getAttribute('loading')).toBe('lazy');
    expect(screen.getByText('800×600 · 2.0 KB · 引用 0 处')).toBeTruthy();
    expect(screen.getByText('1080×1920 · 3.0 MiB · 引用 2 处')).toBeTruthy();
    expect(screen.getByText('_library/guide.jpg')).toBeTruthy();
  });

  test('上传成功时提交文件和可选名称并刷新列表', /* 当前回调确认上传使用 multipart 表单后的唯一刷新。 */ async () => {
    render(<ImageLibrary />);
    await screen.findByText('主图');
    // file 是用户通过文件选择器提供的浏览器文件。
    const file = new File(['png'], 'guide.png', { type: 'image/png' });
    fireEvent.change(screen.getByLabelText('图片文件'), { target: { files: [file] } });
    fireEvent.change(screen.getByLabelText('展示名称（可选）'), { target: { value: ' 发货须知 ' } });
    fireEvent.click(screen.getByRole('button', { name: '上传图片' }));
    await waitFor(/* 当前断言等待上传请求携带文件和名称。 */ () => expect(uploadMock).toHaveBeenCalledExactlyOnceWith(file, ' 发货须知 ', expect.objectContaining({ signal: expect.any(AbortSignal) })));
    await waitFor(/* 当前断言确认上传成功后只刷新一次列表。 */ () => expect(listMock).toHaveBeenCalledTimes(2));
    expect((screen.getByLabelText('展示名称（可选）') as HTMLInputElement).value).toBe('');
  });

  test('改名提交新展示名并刷新列表', /* 当前回调确认行内改名只发送名称字段。 */ async () => {
    render(<ImageLibrary />);
    await screen.findByText('主图');
    fireEvent.click(screen.getByRole('button', { name: '重命名 主图' }));
    // renameInput 是打开后的行内改名输入框，默认带入当前展示名。
    const renameInput = screen.getByLabelText('主图 新名称') as HTMLInputElement;
    expect(renameInput.value).toBe('主图');
    fireEvent.change(renameInput, { target: { value: ' 新主图 ' } });
    fireEvent.click(screen.getByRole('button', { name: '保存名称' }));
    await waitFor(/* 当前断言等待改名请求发送裁剪后的名称。 */ () => expect(renameMock).toHaveBeenCalledExactlyOnceWith(1, '新主图', expect.objectContaining({ signal: expect.any(AbortSignal) })));
    await waitFor(/* 当前断言确认改名后只刷新一次列表。 */ () => expect(listMock).toHaveBeenCalledTimes(2));
    expect(screen.queryByLabelText('主图 新名称')).toBeNull();
  });

  test('未被引用的图片直接删除且不弹出确认', /* 当前回调确认无引用时不需要 force 参数。 */ async () => {
    render(<ImageLibrary />);
    await screen.findByText('主图');
    fireEvent.click(screen.getByRole('button', { name: '删除 主图' }));
    await waitFor(/* 当前断言等待删除请求发送非强制删除。 */ () => expect(deleteMock).toHaveBeenCalledExactlyOnceWith(1, false, expect.objectContaining({ signal: expect.any(AbortSignal) })));
    expect(window.confirm).not.toHaveBeenCalled();
  });

  test('仍被引用的图片需确认后带 force 删除', /* 当前回调确认引用中的图片必须经过二次确认。 */ async () => {
    render(<ImageLibrary />);
    await screen.findByText('教程图');
    fireEvent.click(screen.getByRole('button', { name: '删除 教程图' }));
    await waitFor(/* 当前断言等待强制删除请求。 */ () => expect(deleteMock).toHaveBeenCalledExactlyOnceWith(2, true, expect.objectContaining({ signal: expect.any(AbortSignal) })));
    expect(window.confirm).toHaveBeenCalledTimes(1);
    expect(String(vi.mocked(window.confirm).mock.calls[0][0])).toContain('2 处配置引用');
  });

  test('取消确认时不发送删除请求', /* 当前回调确认用户放弃删除后不产生任何请求。 */ async () => {
    vi.mocked(window.confirm).mockReturnValue(false);
    render(<ImageLibrary />);
    await screen.findByText('教程图');
    fireEvent.click(screen.getByRole('button', { name: '删除 教程图' }));
    await waitFor(/* 当前断言等待确认框被调用。 */ () => expect(window.confirm).toHaveBeenCalledTimes(1));
    expect(deleteMock).not.toHaveBeenCalled();
  });

  test('服务端发现新引用时确认后带 force 重试', /* 当前回调覆盖界面未知引用导致的 409 冲突重试。 */ async () => {
    deleteMock.mockRejectedValueOnce({ status: 409 });
    render(<ImageLibrary />);
    await screen.findByText('主图');
    fireEvent.click(screen.getByRole('button', { name: '删除 主图' }));
    await waitFor(/* 当前断言等待冲突后的强制重试。 */ () => expect(deleteMock).toHaveBeenLastCalledWith(1, true, expect.objectContaining({ signal: expect.any(AbortSignal) })));
    expect(deleteMock).toHaveBeenCalledTimes(2);
    expect(window.confirm).toHaveBeenCalledTimes(1);
  });

  test('加载失败展示错误并允许重试', /* 当前回调确认首屏失败后可以恢复列表。 */ async () => {
    listMock.mockRejectedValueOnce(new Error('网络异常'));
    render(<ImageLibrary />);
    // alert 是首屏请求失败后的用户可见错误区域。
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('加载图片库失败：网络异常');
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByText('主图')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  test('迟到的旧列表响应不覆盖新状态', /* 当前回调确认刷新后的旧代次响应被丢弃。 */ async () => {
    // resolveStale 控制首次列表请求在刷新之后才返回旧数据。
    let resolveStale!: (assets: ImageAsset[]) => void;
    listMock.mockImplementationOnce(/* 当前替身让首屏请求保持在途，且忽略取消信号。 */ () => new Promise<ImageAsset[]>(/* resolve 由测试在刷新完成后调用。 */ resolve => { resolveStale = resolve; }));
    render(<ImageLibrary />);
    fireEvent.click(screen.getByRole('button', { name: '刷新列表' }));
    expect(await screen.findByText('教程图')).toBeTruthy();
    await act(/* 当前回调令首屏旧响应迟到。 */ async () => resolveStale([buildAsset({ id: 9, name: '旧图片', reference: '_library/old.png' })]));
    expect(screen.queryByText('旧图片')).toBeNull();
    expect(screen.getByText('教程图')).toBeTruthy();
  });
});
