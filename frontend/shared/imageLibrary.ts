/** imageLibraryReferencePrefix 是后端约定的资源中心引用前缀，业务配置把 _library/<文件名> 原样保存为 image_path。 */
const imageLibraryReferencePrefix = '_library/';

/** bytesPerKilobyte 是体积换算使用的千字节基数，展示口径与常见文件管理器一致。 */
const bytesPerKilobyte = 1024;

/** bytesPerMegabyte 是体积换算使用的兆字节基数，用于超过 1 MiB 的图片。 */
const bytesPerMegabyte = 1024 * 1024;

/**
 * imageLibraryContentURL 返回资源中心图片内容的版本化地址。
 * 该地址需要登录 Cookie，因此只能用于 <img> 缩略图，不能当作公开外链分享。
 */
export const imageLibraryContentURL = (assetID: number | string): string => `/api/v1/image-library/${encodeURIComponent(String(assetID))}/content`;

/** isLibraryReference 判断配置中的图片路径是否引用资源中心图片；手填的账号目录相对路径返回 false。 */
export const isLibraryReference = (path: string): boolean => (path || '').trim().startsWith(imageLibraryReferencePrefix);

/**
 * formatImageSize 把服务端返回的图片字节数格式化为便于阅读的体积文本。
 * 非有限值或负数按 0 B 处理，避免界面出现 NaN 或负体积。
 */
export const formatImageSize = (bytes: number): string => {
  // safeBytes 是经过有限性和非负校正后的字节数，用于后续所有换算分支。
  const safeBytes = Number.isFinite(bytes) && bytes > 0 ? Math.round(bytes) : 0;
  if (safeBytes < bytesPerKilobyte) return `${safeBytes} B`;
  if (safeBytes < bytesPerMegabyte) return `${(safeBytes / bytesPerKilobyte).toFixed(1)} KB`;
  return `${(safeBytes / bytesPerMegabyte).toFixed(1)} MiB`;
};
