import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

/** 真实 Git 临时仓库验证生成 schema 在 Windows 换行转换开启时仍保留字节级契约校验。 */
describe('generated API schema checkout', () => {
  /** autocrlf 分别模拟关闭换行转换和 Windows 常见的自动 CRLF 检出设置。 */
  it.each(['false', 'true'])('keeps generated schema LF with core.autocrlf=%s', (autocrlf) => {
    /** temporaryRoot 由本测试独占，清理只覆盖 mkdtemp 创建的临时仓库。 */
    const temporaryRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'ydisks-schema-eol-'));
    /** schemaPath 是 .gitattributes 中精确限定的只读生成文件相对路径。 */
    const schemaPath = 'frontend/shared/api-contract/generated/schema.ts';
    /** generated 是生成器已经写入的 LF 产物；检出后必须逐字节保持一致，不能忽略实质类型变更。 */
    const generated = fs.readFileSync(path.resolve('shared/api-contract/generated/schema.ts'));
    try {
      execFileSync('git', ['init', '--quiet', temporaryRoot]);
      execFileSync('git', ['-C', temporaryRoot, 'config', 'core.autocrlf', autocrlf]);
      fs.writeFileSync(path.join(temporaryRoot, '.gitattributes'), fs.readFileSync(path.resolve('../.gitattributes')));
      fs.mkdirSync(path.dirname(path.join(temporaryRoot, schemaPath)), { recursive: true });
      fs.writeFileSync(path.join(temporaryRoot, schemaPath), generated);
      execFileSync('git', ['-C', temporaryRoot, 'add', '--', '.gitattributes', schemaPath], { stdio: 'pipe' });
      fs.unlinkSync(path.join(temporaryRoot, schemaPath));
      execFileSync('git', ['-C', temporaryRoot, 'checkout-index', '--force', '--', schemaPath]);
      expect(fs.readFileSync(path.join(temporaryRoot, schemaPath))).toEqual(generated);
      expect(generated.includes(Buffer.from('\r\n'))).toBe(false);
    } finally {
      fs.rmSync(temporaryRoot, { recursive: true, force: true });
    }
  });
});
