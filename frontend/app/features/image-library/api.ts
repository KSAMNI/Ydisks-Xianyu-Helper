import { contractClient, contractMultipartBody, runContractRequest, type ContractRequestOptions } from '../../../shared/api-contract/client';
import type { ImageLibraryAssetTransport } from '../../../shared/api-contract/imageLibrary';
import { ApiError } from '../../../shared/http/client';
import type { ImageAsset } from './models';

export type * from './models';

/** normalizeImageAsset 把生成的图片契约转换为界面模型，缺失的数值字段收敛为 0。 */
const normalizeImageAsset = (item: ImageLibraryAssetTransport): ImageAsset => ({
  id: Number(item.id) || 0,
  name: item.name || '',
  reference: item.reference || '',
  mimeType: item.mime_type || '',
  sizeBytes: Number(item.size_bytes) || 0,
  width: Number(item.width) || 0,
  height: Number(item.height) || 0,
  usageCount: Number(item.usage_count) || 0,
  createdAt: item.created_at || '',
  updatedAt: item.updated_at || '',
});

/** listImageAssets 读取全局共享的资源中心图片列表。 */
export const listImageAssets = async (options?: ContractRequestOptions): Promise<ImageAsset[]> => {
  // response 是图片列表接口返回的生成契约数据。
  const response = await runContractRequest(/* signal 控制图片列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/image-library', { signal }), options);
  // items 是服务端返回的图片数组；字段缺失时按空列表处理而不是抛错。
  const items: ImageLibraryAssetTransport[] = Array.isArray(response.items) ? response.items : [];
  return items.map(normalizeImageAsset);
};

/** uploadImageAsset 以 multipart 表单上传一张图片，name 省略时由服务端使用原始文件名。 */
export const uploadImageAsset = async (file: File, name?: string, options?: ContractRequestOptions): Promise<ImageAsset> => {
  // body 是保留浏览器 multipart 边界的原生表单，只包含 file 与可选 name 字段。
  const body = new FormData();
  body.append('file', file);
  // trimmedName 是去除首尾空格后的展示名，空值不提交以免覆盖服务端默认命名。
  const trimmedName = (name || '').trim();
  if (trimmedName) body.append('name', trimmedName);
  // asset 是上传成功后返回的单张图片契约数据。
  const asset = await runContractRequest(/* signal 控制图片上传请求的取消和超时。 */ signal => contractClient.POST('/api/v1/image-library', { body: contractMultipartBody(body), signal }), options);
  return normalizeImageAsset(asset);
};

/** renameImageAsset 只修改图片展示名，已有 _library 引用保持不变。 */
export const renameImageAsset = async (assetID: number, name: string, options?: ContractRequestOptions): Promise<ImageAsset> => {
  // asset 是改名成功后返回的最新图片契约数据。
  const asset = await runContractRequest(/* signal 控制图片改名请求的取消和超时。 */ signal => contractClient.PATCH('/api/v1/image-library/{asset_id}', {
    params: { path: { asset_id: String(assetID) } },
    body: { name },
    signal,
  }), options);
  return normalizeImageAsset(asset);
};

/** isImageLibraryConflict 判断失败是否因为图片仍被业务配置引用（HTTP 409 冲突）。 */
export const isImageLibraryConflict = (error: unknown): boolean => error instanceof ApiError && error.status === 409;

/**
 * deleteImageAsset 删除一张资源中心图片；force 表示用户已确认删除仍被引用的图片。
 * 服务端返回统一操作结果对象，success 为假说明删除未完成，必须作为失败处理。
 */
export const deleteImageAsset = async (assetID: number, force = false, options?: ContractRequestOptions): Promise<void> => {
  // response 是删除接口返回的操作结果。
  const response = await runContractRequest(/* signal 控制图片删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/image-library/{asset_id}', {
    params: { path: { asset_id: String(assetID) }, query: force ? { force: '1' as const } : undefined },
    signal,
  }), options);
  if (!response.success) throw new Error('删除资源中心图片失败');
};
