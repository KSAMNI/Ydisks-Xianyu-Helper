import { Images, Pencil, RefreshCw, Trash2, Upload } from 'lucide-react';
import React, { useEffect, useRef, useState } from 'react';
import { formatImageSize, imageLibraryContentURL } from '../../../../shared/imageLibrary';
import { useImageLibrary } from '../hooks';
import type { ImageAsset } from '../models';

/**
 * ImageLibrary 是全局共享的资源中心图片库页面。
 * 页面只负责上传、改名、删除和网格展示；请求取消、代次隔离和刷新都由 Hook 承担。
 */
const ImageLibrary: React.FC = () => {
  // assets、loading、submitting、error 由 Hook 统一管理请求生命周期和竞态保护。
  const { assets, loading, submitting, error, reload, uploadAsset, renameAsset, removeAsset } = useImageLibrary();
  // selectedFile 保存待上传的浏览器文件，null 表示尚未选择文件。
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  // uploadName 是可选展示名输入，空值由服务端使用上传时的原始文件名。
  const [uploadName, setUploadName] = useState('');
  // renamingID 保存正在改名的图片主键，null 表示没有打开的改名输入。
  const [renamingID, setRenamingID] = useState<number | null>(null);
  // renameDraft 保存改名输入框的当前文本，取消时直接丢弃。
  const [renameDraft, setRenameDraft] = useState('');
  // fileInputRef 只用于上传成功后清空浏览器文件选择，不保存业务状态。
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(/* 当前副作用在页面挂载时读取图片库，卸载后由 Hook 取消尚未完成的请求。 */ () => {
    void reload().catch(/* error 是首屏请求失败原因，Hook 已记录用户可见错误。 */ () => undefined);
  }, [reload]);

  // handleUpload 上传当前选择的文件，成功后清空文件选择和名称输入。
  const handleUpload = async (): Promise<void> => {
    if (!selectedFile || submitting) return;
    await uploadAsset(selectedFile, uploadName);
    setSelectedFile(null);
    setUploadName('');
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  // handleStartRename 打开单张图片的行内改名输入，并带入当前展示名。
  const handleStartRename = (asset: ImageAsset): void => {
    setRenamingID(asset.id);
    setRenameDraft(asset.name);
  };

  // handleSubmitRename 提交行内改名，空名称不提交以免产生不可识别条目。
  const handleSubmitRename = async (): Promise<void> => {
    // trimmedName 是去除首尾空格后的新展示名。
    const trimmedName = renameDraft.trim();
    if (renamingID === null || !trimmedName) return;
    await renameAsset(renamingID, trimmedName);
    setRenamingID(null);
    setRenameDraft('');
  };

  // handleDelete 删除图片；仍被引用的确认流程由 Hook 统一处理。
  const handleDelete = async (asset: ImageAsset): Promise<void> => {
    await removeAsset(asset);
  };

  return <div className="space-y-6">
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 className="flex items-center gap-2 text-2xl font-extrabold text-gray-900"><Images className="h-6 w-6 text-brand" />资源中心</h2>
        <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-500">资源中心是所有闲鱼账号共享的图片库。卡密、发货模板和默认回复从这里选择图片后保存 <code>_library/</code> 引用，不再需要把图片分别放到每个账号目录。</p>
      </div>
      <button type="button" onClick={/* 当前回调重新读取图片库，取消旧请求并只接受最新结果。 */ () => void reload().catch(/* error 是刷新失败原因，Hook 已记录用户可见错误。 */ () => undefined)} className="flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2 text-sm font-bold text-gray-700 hover:bg-gray-200"><RefreshCw className="h-4 w-4" />刷新列表</button>
    </div>

    <div className="space-y-3 rounded-2xl border border-slate-200 bg-white p-5">
      <h3 className="text-sm font-bold text-gray-700">上传图片</h3>
      <label className="block text-sm font-bold text-gray-700">
        图片文件
        <input ref={fileInputRef} type="file" accept="image/png,image/jpeg,image/gif" onChange={/* event 保存用户选择的浏览器文件，不立即上传。 */ event => setSelectedFile(event.target.files && event.target.files[0] ? event.target.files[0] : null)} className="mt-2 block w-full text-sm text-gray-600" />
      </label>
      <label className="block text-sm font-bold text-gray-700">
        展示名称（可选）
        <input type="text" value={uploadName} onChange={/* event 是用户输入的展示名，留空时使用原始文件名。 */ event => setUploadName(event.target.value)} placeholder="例如：发货须知长图" className="w-full ios-input px-4 py-3 rounded-xl mt-2" />
      </label>
      <p className="text-xs leading-5 text-gray-500">只接受 PNG、JPEG 或 GIF 图片，单张最大 10 MiB；上传后其他账号配置即可直接选择这张图片。</p>
      <button type="button" onClick={/* 当前回调上传当前选择的文件，成功后清空表单。 */ () => void handleUpload()} disabled={!selectedFile || submitting} className="flex items-center gap-2 rounded-xl bg-brand px-5 py-3 text-sm font-bold text-white disabled:opacity-50"><Upload className="h-4 w-4" />{submitting ? '正在上传…' : '上传图片'}</button>
    </div>

    {error && <div role="alert" className="rounded-xl bg-red-50 p-4 text-sm text-red-700">
      {error}
      <button type="button" onClick={/* 当前回调重新读取列表，用于恢复首屏或刷新失败。 */ () => void reload().catch(/* error 是重试失败原因，Hook 已记录用户可见错误。 */ () => undefined)} className="ml-3 font-bold underline">重试</button>
    </div>}

    {loading && <p role="status" className="text-sm text-gray-500">正在读取图片库…</p>}

    {!loading && assets.length === 0 && !error && <p className="rounded-2xl border border-dashed border-slate-300 p-8 text-center text-sm text-gray-500">资源中心还没有图片，上传后会显示在这里。</p>}

    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {assets.map(/* asset 是当前渲染的资源中心图片。 */ asset => <div key={asset.id} className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4">
        <img src={imageLibraryContentURL(asset.id)} alt={`${asset.name} 缩略图`} loading="lazy" className="h-40 w-full rounded-xl border border-slate-100 object-contain" />
        {renamingID === asset.id ? <div className="space-y-2">
          <label className="block text-xs font-bold text-gray-700">
            新名称
            <input type="text" value={renameDraft} aria-label={`${asset.name} 新名称`} onChange={/* event 是改名输入的当前文本，提交前不修改服务端数据。 */ event => setRenameDraft(event.target.value)} className="mt-1 w-full ios-input px-3 py-2 rounded-lg" />
          </label>
          <div className="flex gap-2">
            <button type="button" aria-label="保存名称" onClick={/* 当前回调提交改名，空名称不会发送请求。 */ () => void handleSubmitRename()} disabled={submitting} className="rounded-lg bg-brand px-3 py-2 text-xs font-bold text-white disabled:opacity-50">保存</button>
            <button type="button" aria-label="取消改名" onClick={/* 当前回调丢弃改名草稿。 */ () => { setRenamingID(null); setRenameDraft(''); }} className="rounded-lg bg-gray-100 px-3 py-2 text-xs font-bold text-gray-700">取消</button>
          </div>
        </div> : <div className="space-y-1">
          <p className="truncate text-sm font-bold text-gray-900">{asset.name}</p>
          <p className="text-xs text-gray-500">{asset.width}×{asset.height} · {formatImageSize(asset.sizeBytes)} · 引用 {asset.usageCount} 处</p>
          <p className="truncate font-mono text-xs text-gray-400">{asset.reference}</p>
          <div className="flex gap-2 pt-1">
            <button type="button" aria-label={`重命名 ${asset.name}`} onClick={/* 当前回调打开该图片的行内改名输入。 */ () => handleStartRename(asset)} className="flex items-center gap-1 rounded-lg bg-gray-100 px-3 py-2 text-xs font-bold text-gray-700 hover:bg-gray-200"><Pencil className="h-3.5 w-3.5" />重命名</button>
            <button type="button" aria-label={`删除 ${asset.name}`} onClick={/* 当前回调删除该图片，仍被引用时先确认再强制删除。 */ () => void handleDelete(asset)} disabled={submitting} className="flex items-center gap-1 rounded-lg bg-red-50 px-3 py-2 text-xs font-bold text-red-600 hover:bg-red-100 disabled:opacity-50"><Trash2 className="h-3.5 w-3.5" />删除</button>
          </div>
        </div>}
      </div>)}
    </div>
  </div>;
};

export default ImageLibrary;
