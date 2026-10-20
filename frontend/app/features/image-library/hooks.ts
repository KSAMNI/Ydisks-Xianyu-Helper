import { useCallback, useEffect, useRef, useState } from 'react';
import { deleteImageAsset, isImageLibraryConflict, listImageAssets, renameImageAsset, uploadImageAsset } from './api';
import type { ImageAsset } from './models';

/** ImageLibraryHookResult 描述资源中心页面需要的列表、变更和错误状态。 */
export type ImageLibraryHookResult = {
  /** assets 是当前最新列表请求返回的图片，旧代次响应不会写入该字段。 */
  assets: ImageAsset[];
  /** loading 表示列表请求是否正在等待。 */
  loading: boolean;
  /** submitting 表示上传、改名或删除请求是否正在等待，用于禁用重复提交。 */
  submitting: boolean;
  /** error 是最近一次真实请求失败的用户可见错误。 */
  error: string;
  /** reload 取消旧列表请求并重新读取图片库。 */
  reload: () => Promise<void>;
  /** uploadAsset 上传一张图片并在成功后刷新列表。 */
  uploadAsset: (file: File, name: string) => Promise<void>;
  /** renameAsset 修改图片展示名并在成功后刷新列表。 */
  renameAsset: (assetID: number, name: string) => Promise<void>;
  /** removeAsset 删除图片；仍被引用时先由用户确认再带 force 重试。 */
  removeAsset: (asset: ImageAsset) => Promise<void>;
};

/** isAbortError 判断用户取消导致的请求错误，避免主动取消时弹出失败提示。 */
const isAbortError = (error: unknown): boolean => error instanceof Error && error.message.includes('取消');

/** useImageLibrary 集中管理图片库请求的取消、最新代次和变更后的唯一刷新。 */
export const useImageLibrary = (): ImageLibraryHookResult => {
  // assets 保存当前唯一有效列表请求返回的图片数据。
  const [assets, setAssets] = useState<ImageAsset[]>([]);
  // loading 表示首屏或重试列表请求正在进行。
  const [loading, setLoading] = useState(true);
  // submitting 表示上传、改名或删除请求正在进行。
  const [submitting, setSubmitting] = useState(false);
  // error 保存最近一次真实请求失败的用户可见错误。
  const [error, setError] = useState('');
  // listControllerRef 保存当前列表请求控制器，开始新列表请求时先取消旧请求。
  const listControllerRef = useRef<AbortController | null>(null);
  // actionControllerRef 保存当前变更请求控制器，卸载时统一取消。
  const actionControllerRef = useRef<AbortController | null>(null);
  // generationRef 标记最新列表请求代次，迟到的旧响应不能覆盖当前页面状态。
  const generationRef = useRef(0);
  // mountedRef 防止卸载后的异步收尾继续修改 React 状态。
  const mountedRef = useRef(true);

  // reload 取消旧列表请求并只允许最新代次写入页面状态。
  const reload = useCallback(/* reloadCallback 取消旧列表请求并只接受最新代次的结果。 */ async (): Promise<void> => {
    // generation 保存本次列表请求的最新代次。
    const generation = ++generationRef.current;
    listControllerRef.current?.abort();
    // controller 控制本次列表请求的取消生命周期。
    const controller = new AbortController();
    listControllerRef.current = controller;
    if (mountedRef.current) {
      setLoading(true);
      setError('');
    }
    try {
      // result 保存本次列表请求返回的图片数据。
      const result = await listImageAssets({ signal: controller.signal });
      if (!controller.signal.aborted && mountedRef.current && generation === generationRef.current) setAssets(result);
    } catch (/* error 是本次列表请求失败原因。 */ listError) {
      if (!isAbortError(listError) && mountedRef.current && generation === generationRef.current) {
        setError(`加载图片库失败：${(listError as Error).message}`);
        throw listError;
      }
    } finally {
      if (mountedRef.current && generation === generationRef.current) setLoading(false);
    }
  }, []);

  // runMutation 串行执行一次变更请求，成功后刷新一次列表，失败时只记录用户可见错误。
  const runMutation = useCallback(/* runMutationCallback 串行执行一次变更请求并在成功后刷新列表。 */ async (mutation: (signal: AbortSignal) => Promise<void>): Promise<void> => {
    if (actionControllerRef.current) return;
    // controller 控制本次变更请求的取消生命周期。
    const controller = new AbortController();
    actionControllerRef.current = controller;
    if (mountedRef.current) {
      setSubmitting(true);
      setError('');
    }
    try {
      await mutation(controller.signal);
      if (!controller.signal.aborted && mountedRef.current) {
        try {
          await reload();
        } catch {
          // 变更本身已经成功，刷新失败已由 reload 记录为界面错误，这里不再覆盖成功结果。
        }
      }
    } catch (/* mutationError 是本次变更请求失败原因。 */ mutationError) {
      if (!isAbortError(mutationError) && mountedRef.current) setError((mutationError as Error).message);
    } finally {
      if (actionControllerRef.current === controller) {
        actionControllerRef.current = null;
        if (mountedRef.current) setSubmitting(false);
      }
    }
  }, [reload]);

  // uploadAsset 上传浏览器选择的图片文件，名称留空时由服务端使用原始文件名。
  const uploadAsset = useCallback(/* uploadCallback 绑定当前变更串行化逻辑。 */ async (file: File, name: string): Promise<void> => {
    await runMutation(/* uploadMutation 在变更信号下上传图片。 */ async signal => {
      await uploadImageAsset(file, name, { signal });
    });
  }, [runMutation]);

  // renameAsset 只修改展示名，已有 _library 引用保持不变。
  const renameAsset = useCallback(/* renameCallback 绑定当前变更串行化逻辑。 */ async (assetID: number, name: string): Promise<void> => {
    await runMutation(/* renameMutation 在变更信号下修改展示名。 */ async signal => {
      await renameImageAsset(assetID, name, { signal });
    });
  }, [runMutation]);

  // removeAsset 删除图片：已知被引用时先确认，服务端返回冲突时再次确认并带 force 重试。
  const removeAsset = useCallback(/* removeCallback 绑定当前变更串行化逻辑。 */ async (asset: ImageAsset): Promise<void> => {
    // needsForce 表示界面已知该图片仍被业务配置引用。
    const needsForce = asset.usageCount > 0;
    if (needsForce && !window.confirm(`「${asset.name}」仍被 ${asset.usageCount} 处配置引用，删除后这些配置将无法再发送该图片。确定删除？`)) return;
    await runMutation(/* removeMutation 在变更信号下执行删除和必要的强制重试。 */ async signal => {
      try {
        await deleteImageAsset(asset.id, needsForce, { signal });
      } catch (/* deleteError 是首次删除失败原因，服务端发现新引用时返回 409 冲突。 */ deleteError) {
        if (needsForce || !isImageLibraryConflict(deleteError)) throw deleteError;
        if (!window.confirm(`「${asset.name}」已被业务配置引用，删除后这些配置将无法再发送该图片。确定删除？`)) return;
        await deleteImageAsset(asset.id, true, { signal });
      }
    });
  }, [runMutation]);

  useEffect(/* 当前副作用管理挂载状态，并在卸载时取消请求和推进代次。 */ () => {
    mountedRef.current = true;
    return /* 当前清理函数释放所有请求控制器并推进代次。 */ () => {
      mountedRef.current = false;
      ++generationRef.current;
      listControllerRef.current?.abort();
      actionControllerRef.current?.abort();
    };
  }, []);

  return { assets, loading, submitting, error, reload, uploadAsset, renameAsset, removeAsset };
};
