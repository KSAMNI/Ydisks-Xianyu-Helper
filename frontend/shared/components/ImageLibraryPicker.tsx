import type { ReactNode } from 'react';
import { imageLibraryContentURL } from '../imageLibrary';
import { useImageLibraryOptions } from './ImageLibraryContext';

/** ImageLibraryPickerProps 描述受控图片引用控件；value 始终是业务配置中保存的 image_path 原文。 */
export interface ImageLibraryPickerProps {
  /** value 是当前保存的图片引用：资源中心 reference 或历史手填的账号目录相对路径。 */
  value: string;
  /** onChange 回传用户选中的 reference 或手填路径原文，由调用方写入自己的草稿字段。 */
  onChange: (next: string) => void;
  /** label 是手填输入框的无障碍标签，默认沿用旧控件的“本地图片相对路径”。 */
  label?: string;
  /** helpId 是帮助文案容器 id，两个控件都通过 aria-describedby 关联同一段说明。 */
  helpId?: string;
  /** help 覆盖默认帮助文案，供需要按账号目录说明的调用方替换通用说明。 */
  help?: ReactNode;
}

/** defaultImageLibraryHelp 说明资源中心共享图片与历史账号目录引用的关系，并保留格式和体积边界。 */
const defaultImageLibraryHelp = <>
  <p>资源中心图片在“资源中心”页面上传，所有闲鱼账号共用一份；这里下拉选中的就是该图片。每次发送读取 PNG、JPEG 或 GIF 图片，最大 10 MiB。</p>
  <p>手填路径只用于兼容历史配置：图片必须放在运行程序机器的 <code>XIANYU_UPLOAD_DIR/reply-images/&lt;实际发货账号&gt;/</code> 内，默认目录为 <code>data/uploads/reply-images/&lt;实际发货账号&gt;/</code>，使用 / 分隔；Docker 需挂载该目录。</p>
</>;

/**
 * ImageLibraryPicker 编辑一处图片引用：下拉选择全局共享的资源中心图片，同时保留手填相对路径的兼容输入。
 * 控件不发起任何请求，缩略图直接使用受登录保护的图片内容地址。
 */
export function ImageLibraryPicker({ value, onChange, label = '本地图片相对路径', helpId, help }: ImageLibraryPickerProps) {
  // options 是 Provider 提供的资源中心图片；缺少 Provider 时为空数组，控件退化为纯手填输入。
  const options = useImageLibraryOptions();
  // trimmedValue 去掉手填值首尾空格后用于匹配资源中心引用，不改变最终提交的原文。
  const trimmedValue = (value || '').trim();
  // matchedOption 是当前值与资源中心引用完全一致的选项，只有命中时才展示缩略图。
  const matchedOption = options.find(/* option 是当前待比较的资源中心图片。 */ option => option.reference === trimmedValue);
  // showHistoricalOption 表示当前值不在资源中心列表内，需要额外保留一个历史选项避免下拉丢失旧配置。
  const showHistoricalOption = Boolean(trimmedValue) && !matchedOption;
  return <div className="space-y-2">
    <label className="block text-sm font-bold text-gray-700">
      资源中心图片
      <select value={trimmedValue} onChange={/* event 是用户选择的图片引用，空字符串表示清空资源中心图片。 */ event => onChange(event.target.value)} aria-describedby={helpId} className="w-full ios-input px-4 py-3 rounded-xl mt-2">
        <option value="">未选择资源中心图片</option>
        {options.map(/* option 是资源中心图片候选，value 保存可直接写入配置的引用文本。 */ option => <option key={option.id} value={option.reference} title={option.reference}>{option.name}</option>)}
        {showHistoricalOption && <option value={trimmedValue}>当前值（不在资源中心）</option>}
      </select>
    </label>
    {matchedOption && <img src={imageLibraryContentURL(matchedOption.id)} alt={`${matchedOption.name} 缩略图`} loading="lazy" className="h-16 w-16 rounded-xl border border-slate-200 object-cover" />}
    <label className="block text-sm font-bold text-gray-700">
      {label}
      <input type="text" value={value} onChange={/* event 是用户手填的相对路径原文，直接写入配置以兼容历史目录引用。 */ event => onChange(event.target.value)} placeholder="product-a/guide.jpg" aria-describedby={helpId} className="w-full ios-input px-4 py-3 rounded-xl mt-2" />
    </label>
    <div id={helpId} className="space-y-1 text-xs leading-5 text-gray-500 break-all">{help ?? defaultImageLibraryHelp}</div>
  </div>;
}
