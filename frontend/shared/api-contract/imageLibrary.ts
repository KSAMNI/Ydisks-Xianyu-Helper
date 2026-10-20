// imageLibrary 只公开 OpenAPI 生成的资源中心图片传输类型；图片展示模型属于 image-library feature。
import type { components } from './generated/schema';

/** ImageLibraryAssetTransport 表示生成的单张资源中心图片，字段名保持服务端 snake_case 契约。 */
export type ImageLibraryAssetTransport = components['schemas']['ImageLibraryAssetResponse'];

/** ImageLibraryListTransport 表示生成的图片列表响应，items 是唯一的图片数组字段。 */
export type ImageLibraryListTransport = components['schemas']['ImageLibraryListResponse'];

/** ImageLibraryRenameTransport 表示生成的图片重命名请求体，只允许修改展示名。 */
export type ImageLibraryRenameTransport = components['schemas']['ImageLibraryRenameRequest'];
