import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

/** assetSource 是被Vite按原始字节哈希的SVG输入，必须在所有宿主保留相同换行。 */
const assetSource = resolve(__dirname, 'assets/squircle.svg');
/** attributesPath 是控制Windows检出换行的仓库属性文件。 */
const attributesPath = resolve(__dirname, '../.gitattributes');

describe('Vite SVG输入的跨平台字节稳定性', /* 当前回调保护静态构建输入，不验证或替代业务行为。 */ () => {
  test('SVG工作树使用LF且Git明确禁止检出CRLF', /* 当前回调同时检查实际输入与未来检出策略，避免仅重建一次掩盖问题。 */ () => {
    // source 是Vite实际读取的原始SVG文本，不能靠生成物归一来掩盖差异。
    const source = readFileSync(assetSource, 'utf8');
    // attributes 固定到单个哈希敏感资源，不扩大到无关代码或全部图片。
    const attributes = readFileSync(attributesPath, 'utf8');
    expect(source).not.toContain('\r');
    expect(source).toContain('\n');
    expect(attributes).toMatch(/^frontend\/assets\/squircle\.svg\s+text\s+eol=lf\s*$/m);
  });
});
