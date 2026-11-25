import { Loader2,X } from 'lucide-react';
import React from 'react';
import { useImageLibraryOptions } from '../../../../shared/components/ImageLibraryContext';
import { imageLibraryContentURL } from '../../../../shared/imageLibrary';
import type { ChatLibraryImageAsset } from '../api';

/** ChatLibraryImagePickerProps 描述聊天输入区资源中心图片面板所需的状态与回调。 */
type ChatLibraryImagePickerProps = {
  /** open 控制面板是否可见；收起时不渲染任何节点。 */
  open: boolean;
  /** loadingAssetID 是正在读取内容的图片标识，空值表示当前没有读取中的图片。 */
  loadingAssetID: number | null;
  /** disabled 表示账号离线或正在发送，面板中的图片不可选择。 */
  disabled: boolean;
  /** onSelect 把用户选中的资源中心图片交给聊天发送流程。 */
  onSelect: (asset: ChatLibraryImageAsset) => void;
  /** onClose 收起面板且不改变聊天草稿。 */
  onClose: () => void;
};

/**
 * ChatLibraryImagePicker 在聊天输入区展示资源中心（消息图片）的缩略图网格。
 * 组件只负责选择与加载反馈，图片内容读取和发送仍由聊天 Hook 的既有发送流程负责。
 */
const ChatLibraryImagePicker: React.FC<ChatLibraryImagePickerProps> = ({ open, loadingAssetID, disabled, onSelect, onClose }) => {
  // options 是应用层 Provider 提供的资源中心图片选项；缺少 Provider 时为空数组。
  const options = useImageLibraryOptions();
  if (!open) return null;
  // busy 表示已有图片正在读取，此时禁止再次提交选择，避免并发送图请求。
  const busy = loadingAssetID !== null;
  return <div role="dialog" aria-label="资源中心图片" className="absolute bottom-11 left-0 z-30 w-[360px] max-w-[92vw] rounded-2xl border border-slate-200 bg-white p-3 shadow-2xl">
    <div className="mb-2 flex items-center justify-between">
      <div className="text-xs font-bold text-slate-500">资源中心图片</div>
      <button type="button" onClick={/* 当前回调收起资源中心图片面板。 */ onClose} className="rounded-lg p-1 text-slate-400 transition hover:bg-slate-100 hover:text-slate-700" aria-label="关闭资源中心图片"><X className="h-4 w-4" /></button>
    </div>
    {options.length === 0
      ? <div className="px-2 py-8 text-center text-xs leading-5 text-slate-400">资源中心还没有图片。先在“资源中心 → 消息图片”上传，所有闲鱼账号共用一份。</div>
      : <div className="grid max-h-72 grid-cols-3 gap-2 overflow-y-auto">
        {options.map(/* option 是当前待展示的资源中心图片选项。 */ option => <button key={option.id} type="button" title={option.name} disabled={disabled || busy}
          onClick={/* 当前回调提交选中的资源中心图片。 */ () => onSelect({ id: option.id, name: option.name })}
          className="group relative overflow-hidden rounded-xl border border-slate-200 bg-slate-50 transition hover:border-sky-300 disabled:cursor-not-allowed disabled:opacity-50">
          <img src={imageLibraryContentURL(option.id)} alt={option.name} loading="lazy" className="h-20 w-full object-cover" />
          {loadingAssetID === option.id && <span className="absolute inset-0 flex items-center justify-center bg-white/70"><Loader2 className="h-5 w-5 animate-spin text-sky-500" aria-label="正在读取图片" /></span>}
          <span className="block truncate px-1.5 py-1 text-[10px] font-medium text-slate-500">{option.name}</span>
        </button>)}
      </div>}
  </div>;
};

export default ChatLibraryImagePicker;
