/** ImageAsset 是资源中心图片的界面模型，字段已转换为前端 camelCase，不再暴露传输层字段名。 */
export interface ImageAsset {
  /** id 是图片主键，用于拼接受登录保护的缩略图地址和变更请求路径。 */
  id: number;
  /** name 是用户可见的展示名，只影响展示，不改变已有引用。 */
  name: string;
  /** reference 是业务配置必须保存的 _library/<文件名> 引用文本。 */
  reference: string;
  /** mimeType 是服务端确认的图片格式，只用于展示。 */
  mimeType: string;
  /** sizeBytes 是图片原始字节数，用于展示体积。 */
  sizeBytes: number;
  /** width 是图片像素宽度，用于展示尺寸。 */
  width: number;
  /** height 是图片像素高度，用于展示尺寸。 */
  height: number;
  /** usageCount 是当前仍引用该图片的卡密、模板和默认回复数量。 */
  usageCount: number;
  /** createdAt 是服务端记录的创建时间文本，原样展示不做时区换算。 */
  createdAt: string;
  /** updatedAt 是服务端记录的最后更新时间文本，原样展示不做时区换算。 */
  updatedAt: string;
}
