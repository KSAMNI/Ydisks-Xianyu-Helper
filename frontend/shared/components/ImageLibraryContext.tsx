import { createContext, useContext } from 'react';

/** ImageLibraryOption 是图片下拉需要的最小字段集合，不携带体积、尺寸等页面展示细节。 */
export interface ImageLibraryOption {
  /** id 是资源中心图片主键，用于拼接受登录保护的缩略图地址。 */
  id: number;
  /** name 是用户可见的图片展示名。 */
  name: string;
  /** reference 是业务配置必须保存的 _library/<文件名> 引用文本。 */
  reference: string;
}

/**
 * ImageLibraryContext 保存当前用户可见的资源中心图片选项。
 * 默认值为空数组，使没有 Provider 的既有单元测试和独立组件渲染都能安全降级。
 */
export const ImageLibraryContext = createContext<readonly ImageLibraryOption[]>([]);

/** useImageLibraryOptions 读取图片选项；缺少 Provider 时返回空数组而不是抛错。 */
export const useImageLibraryOptions = (): readonly ImageLibraryOption[] => useContext(ImageLibraryContext);
