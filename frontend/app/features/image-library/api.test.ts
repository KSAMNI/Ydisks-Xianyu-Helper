import { afterEach, describe, expect, test, vi } from 'vitest';
import { deleteImageAsset, isImageLibraryConflict, listImageAssets, renameImageAsset, uploadImageAsset } from './api';

/** stubContractFetch 把契约客户端发出的 Request 归一成可断言的路径与请求体，避免测试重建 FormData。 */
const stubContractFetch = (fetchMock: typeof fetch): void => {
  vi.stubGlobal('fetch', /* stubFetch 归一契约客户端请求并交给用例提供的替身。 */ async (input: RequestInfo | URL, init?: RequestInit) => {
    // request 是 openapi-fetch 已经序列化的请求，测试只读取克隆内容。
    const request = input instanceof Request ? input : new Request(input, init);
    // requestURL 用于把绝对占位地址还原为断言使用的版本化路径。
    const requestURL = new URL(request.url);
    // contentType 决定请求体按表单还是文本读取。
    const contentType = request.headers.get('content-type') || '';
    // body 是仅供断言使用的克隆请求体，生产请求不会被消费。
    const body: BodyInit | undefined = request.method === 'GET'
      ? undefined
      : contentType.includes('multipart/form-data')
        ? await request.clone().formData()
        : await request.clone().text();
    return fetchMock(`${requestURL.pathname}${requestURL.search}`, { method: request.method, body });
  });
};

/** jsonResponse 构造带 JSON 内容类型的响应，状态码可按用例覆盖。 */
const jsonResponse = (body: unknown, status = 200): Response => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

/** assetPayload 是服务端返回的单张图片契约数据。 */
const assetPayload = { id: 4, name: '发货图', reference: '_library/abc.png', mime_type: 'image/png', size_bytes: 2048, width: 640, height: 480, usage_count: 2, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-02T00:00:00Z' };

afterEach(/* 当前回调清理全局 fetch 替身。 */ () => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('资源中心图片 API 适配器', /* 当前回调验证契约路径、模型转换、multipart 上传与删除结果。 */ () => {
  test('列表读取 items 并转换为界面模型', /* 当前回调确认 snake_case 字段只在适配层转换。 */ async () => {
    // fetchMock 返回一条图片契约数据。
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [assetPayload] }));
    stubContractFetch(fetchMock);
    await expect(listImageAssets()).resolves.toEqual([{ id: 4, name: '发货图', reference: '_library/abc.png', mimeType: 'image/png', sizeBytes: 2048, width: 640, height: 480, usageCount: 2, createdAt: '2024-01-01T00:00:00Z', updatedAt: '2024-01-02T00:00:00Z' }]);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/image-library');
  });

  test('缺少 items 字段时按空列表处理', /* 当前回调确认空图片库不是异常。 */ async () => {
    stubContractFetch(vi.fn().mockResolvedValue(jsonResponse({})));
    await expect(listImageAssets()).resolves.toEqual([]);
  });

  test('上传使用 multipart 表单提交文件与可选名称', /* 当前回调确认浏览器 FormData 边界由共享客户端保留。 */ async () => {
    // fetchMock 返回上传成功后的图片契约数据。
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(assetPayload, 201));
    stubContractFetch(fetchMock);
    // file 是用户选择的浏览器文件。
    const file = new File(['png'], 'abc.png', { type: 'image/png' });
    await expect(uploadImageAsset(file, ' 发货图 ')).resolves.toMatchObject({ reference: '_library/abc.png' });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/image-library');
    // form 是测试替身读到的原始表单，用于断言文件与名称字段。
    const form = fetchMock.mock.calls[0][1].body as FormData;
    expect((form.get('file') as File).name).toBe('abc.png');
    expect(form.get('name')).toBe('发货图');
  });

  test('上传时省略空名称字段', /* 当前回调确认空展示名交给服务端使用原始文件名。 */ async () => {
    // fetchMock 返回上传成功后的图片契约数据。
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(assetPayload, 201));
    stubContractFetch(fetchMock);
    await uploadImageAsset(new File(['png'], 'abc.png', { type: 'image/png' }), '   ');
    // form 是测试替身读到的原始表单，空名称不得出现在请求里。
    const form = fetchMock.mock.calls[0][1].body as FormData;
    expect(form.get('name')).toBeNull();
  });

  test('改名只提交新的展示名', /* 当前回调确认引用文本不随改名变化。 */ async () => {
    // fetchMock 返回改名后的图片契约数据。
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(assetPayload));
    stubContractFetch(fetchMock);
    await expect(renameImageAsset(4, '新名称')).resolves.toMatchObject({ id: 4 });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/image-library/4');
    expect(fetchMock.mock.calls[0][1].method).toBe('PATCH');
    expect(JSON.parse(String(fetchMock.mock.calls[0][1].body))).toEqual({ name: '新名称' });
  });

  test('删除返回统一操作结果并按需带 force', /* 当前回调确认删除成功由 success 字段判定。 */ async () => {
    // fetchMock 依次返回普通删除和强制删除的成功操作结果。
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ success: true }))
      .mockResolvedValueOnce(jsonResponse({ success: true }));
    stubContractFetch(fetchMock);
    await expect(deleteImageAsset(4)).resolves.toBeUndefined();
    await expect(deleteImageAsset(4, true)).resolves.toBeUndefined();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/image-library/4');
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/image-library/4?force=1');
  });

  test('删除返回 success=false 时视为失败', /* 当前回调确认未完成删除不能静默成功。 */ async () => {
    stubContractFetch(vi.fn().mockResolvedValue(jsonResponse({ success: false })));
    await expect(deleteImageAsset(4)).rejects.toThrow('删除资源中心图片失败');
  });

  test('删除冲突可被识别为 409', /* 当前回调确认界面据此请求用户二次确认。 */ async () => {
    stubContractFetch(vi.fn().mockResolvedValue(jsonResponse({ code: 'image_in_use', message: '图片仍被引用' }, 409)));
    // conflict 是删除失败抛出的统一错误，用于校验冲突判定。
    const conflict = await deleteImageAsset(4).catch(/* error 是删除失败原因。 */ error => error);
    expect(isImageLibraryConflict(conflict)).toBe(true);
    expect(isImageLibraryConflict(new Error('其他失败'))).toBe(false);
  });
});
