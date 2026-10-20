import { describe, expect, test } from 'vitest';
import { formatImageSize, imageLibraryContentURL, isLibraryReference } from './imageLibrary';

describe('资源中心图片共享工具', /* 当前回调验证缩略图地址、引用前缀和体积展示。 */ () => {
  test('图片内容地址使用版本化路径并转义标识', /* 当前回调确认 <img> 使用受登录保护的 v1 地址。 */ () => {
    expect(imageLibraryContentURL(12)).toBe('/api/v1/image-library/12/content');
    expect(imageLibraryContentURL('a/b')).toBe('/api/v1/image-library/a%2Fb/content');
  });

  test('只有 _library 前缀被识别为资源中心引用', /* 当前回调确认历史手填路径不会被误判。 */ () => {
    expect(isLibraryReference('_library/abc.png')).toBe(true);
    expect(isLibraryReference('  _library/abc.png  ')).toBe(true);
    expect(isLibraryReference('goods/abc.png')).toBe(false);
    expect(isLibraryReference('')).toBe(false);
    expect(isLibraryReference('library/abc.png')).toBe(false);
  });

  test('体积按 B、KB、MiB 展示并对非法输入收敛为 0 B', /* 当前回调覆盖换算边界与异常值。 */ () => {
    expect(formatImageSize(0)).toBe('0 B');
    expect(formatImageSize(512)).toBe('512 B');
    expect(formatImageSize(1024)).toBe('1.0 KB');
    expect(formatImageSize(1536)).toBe('1.5 KB');
    expect(formatImageSize(1024 * 1024)).toBe('1.0 MiB');
    expect(formatImageSize(10 * 1024 * 1024)).toBe('10.0 MiB');
    expect(formatImageSize(Number.NaN)).toBe('0 B');
    expect(formatImageSize(-5)).toBe('0 B');
    expect(formatImageSize(Number.POSITIVE_INFINITY)).toBe('0 B');
  });
});
