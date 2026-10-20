import { useId } from 'react';
import { imageSource, type ImageSourceDraft } from '../imageSource';
import { ImageLibraryPicker } from './ImageLibraryPicker';

/** ImageSourceEditorProps 不依赖任何 feature，只编辑图片引用。 */
interface ImageSourceEditorProps {
  /** value 是独立表单中的图片来源草稿。 */
  value: ImageSourceDraft;
  /** onChange 返回完整来源字段，切换时同时清空两种旧输入。 */
  onChange: (value: ImageSourceDraft) => void;
}

/** ImageSourceEditor 共用 URL/服务器路径控件，不上传浏览器文件，本地图片从资源中心选择或手填相对路径。 */
export function ImageSourceEditor({ value, onChange }: ImageSourceEditorProps) {
  // helpID 让多消息编辑器中的各个路径输入独立关联说明。
  const helpID = useId();
  // source 保留用户选择但尚未输入路径的状态。
  const source = imageSource(value);
  return <div className="space-y-3">
    <label className="block text-sm font-bold text-gray-700">
      图片来源
      <select value={source} onChange={/* event 切换来源并丢弃旧输入，禁止两个来源并存。 */ event => onChange({ image_source: event.target.value as 'url' | 'local', image_url: '', image_path: '' })} className="w-full ios-input px-4 py-3 rounded-xl mt-2">
        <option value="url">图片 URL</option><option value="local">服务器本地图片</option>
      </select>
    </label>
    {source === 'url' ? <label className="block text-sm font-bold text-gray-700">
      图片 URL
      <input type="url" value={value.image_url || ''} onChange={/* event 仅更新远程图片引用，不发起浏览器预览请求。 */ event => onChange({ image_source: 'url', image_url: event.target.value, image_path: '' })} placeholder="https://example.com/image.jpg" className="w-full ios-input px-4 py-3 rounded-xl mt-2" />
    </label> : <ImageLibraryPicker
      value={value.image_path || ''}
      onChange={/* path 是资源中心引用或手填的相对路径，来源固定为服务器本地图片。 */ path => onChange({ image_source: 'local', image_url: '', image_path: path })}
      label="本地图片相对路径"
      helpId={helpID}
    />}
  </div>;
}
