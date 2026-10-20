import { describe, expect, test } from 'vitest';
import { CurlImportError, parseCurlCommand } from './curlImport';

describe('parseCurlCommand', /* suiteCallback 覆盖 cURL 导入到 API 卡密表单时的解析和拒绝边界。 */ () => {
  // test 验证常见 JSON POST 的地址、认证请求头和动态变量会完整保留。
  test('导入带 JSON 正文、请求头和 URL 查询参数的 POST 请求', /* jsonImportCallback 断言 JSON POST 的完整字段映射。 */ () => {
    // config 保存粘贴命令转换后的 API 卡密表单字段。
    const config = parseCurlCommand(`curl -X POST 'https://api.example.com/code?shop=main' \\
      -H 'Authorization: Bearer sample-token' \\
      -H 'Content-Type: application/json; charset=utf-8' \\
      --data '{"order_id":"{order_id}","quantity":1}'`);
    expect(config).toEqual({
      url: 'https://api.example.com/code',
      method: 'POST',
      headers: JSON.stringify({ Authorization: 'Bearer sample-token' }),
      params: JSON.stringify({ shop: 'main' }),
      contentType: 'application/json',
      body: '{\n  "order_id": "{order_id}",\n  "quantity": 1\n}',
    });
  });

  // test 验证 cURL 默认表单正文会写入 URL 编码正文编辑器可消费的键值对象。
  test('导入默认表单 POST 并合并多段 data 参数', /* formImportCallback 断言多段表单正文的合并结果。 */ () => {
    // config 保存多段 data 归一后的 API 卡密表单字段。
    const config = parseCurlCommand("curl https://api.example.com/code -d 'order_id={order_id}' --data 'quantity=2'");
    expect(config).toEqual(expect.objectContaining({
      method: 'POST',
      contentType: 'application/x-www-form-urlencoded',
      body: JSON.stringify({ order_id: '{order_id}', quantity: '2' }),
    }));
  });

  // test 验证 -G 会把 data 转为查询参数，而不是产生无法由 GET 发送的请求正文。
  test('导入 -G GET 请求并合并 URL 与 data 查询参数', /* getImportCallback 断言 -G 查询参数映射。 */ () => {
    // config 保存 GET 查询参数归一后的 API 卡密表单字段。
    const config = parseCurlCommand("curl -G 'https://api.example.com/code?shop=old' --data 'shop=new&order_id={order_id}'");
    expect(config).toEqual(expect.objectContaining({
      url: 'https://api.example.com/code',
      method: 'GET',
      params: JSON.stringify({ shop: 'new', order_id: '{order_id}' }),
      body: '',
    }));
  });

  // test 验证不可由现有卡密 API 无损发送的 cURL 功能会给出明确失败，而非默默改变请求。
  test('拒绝 multipart 和不支持的方法', /* unsupportedCallback 断言不可无损发送的 cURL 不会被静默篡改。 */ () => {
    expect(/* multipartCall 触发 multipart 导入的失败分支。 */ () => parseCurlCommand("curl -F 'file=@card.txt' https://api.example.com/code")).toThrow(CurlImportError);
    expect(/* putCall 触发不支持 HTTP 方法的失败分支。 */ () => parseCurlCommand('curl -X PUT https://api.example.com/code')).toThrow('当前 API 卡密只支持 GET 或 POST');
    expect(/* rawTextCall 触发不能由键值编辑器无损表达的原始文本正文分支。 */ () => parseCurlCommand("curl https://api.example.com/code -H 'Content-Type: text/plain' -d 'raw body'")).toThrow('不能无损导入');
  });
});
