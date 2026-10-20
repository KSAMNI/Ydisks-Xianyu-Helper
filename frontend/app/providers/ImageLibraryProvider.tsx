import React, { useEffect, useState } from 'react';
import { ImageLibraryContext, type ImageLibraryOption } from '../../shared/components/ImageLibraryContext';
import { listImageAssets } from '../features/image-library/api';
import type { ImageAsset } from '../features/image-library/models';
import { useSession } from './SessionProvider';

/** toImageLibraryOption 把图片界面模型裁剪成下拉选项，避免把体积等展示字段放进全局上下文。 */
const toImageLibraryOption = (asset: ImageAsset): ImageLibraryOption => ({ id: asset.id, name: asset.name, reference: asset.reference });

/**
 * ImageLibraryProvider 在已登录时读取一次资源中心图片，并向全应用提供只读选项。
 * 读取失败时静默降级为空列表，不阻塞任何页面渲染；未登录时不发请求，避免触发全局登出。
 */
export const ImageLibraryProvider: React.FC<React.PropsWithChildren> = ({ children }) => {
  // isLoggedIn 是会话 Provider 拥有的服务端状态，只用于决定是否读取图片库。
  const { isLoggedIn } = useSession();
  // options 保存全局共享的图片选项，引用稳定，避免消费者无意义重渲染。
  const [options, setOptions] = useState<ImageLibraryOption[]>([]);

  useEffect(/* 当前副作用在登录状态变化时读取或清空图片库，并在卸载时取消在途请求。 */ () => {
    if (!isLoggedIn) {
      setOptions([]);
      return;
    }
    // controller 取消登录状态变化或卸载后不再需要的图片列表请求。
    const controller = new AbortController();
    // active 保证迟到的旧响应不能覆盖已经变化的状态。
    let active = true;
    listImageAssets({ signal: controller.signal, skipAuthLogout: true })
      .then(/* assets 是当前用户可见的资源中心图片。 */ assets => {
        if (active) setOptions(assets.map(toImageLibraryOption));
      })
      .catch(/* error 是图片列表读取失败原因，静默降级为空列表且不阻塞界面。 */ () => {
        if (active) setOptions([]);
      });
    return /* 当前清理函数取消请求并阻止迟到结果写入状态。 */ () => {
      active = false;
      controller.abort();
    };
  }, [isLoggedIn]);

  return <ImageLibraryContext.Provider value={options}>{children}</ImageLibraryContext.Provider>;
};
