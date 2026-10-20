import { expect, test } from 'vitest';
import { apiConfigFromEditForm, updateAPIEditForm } from './editConfig';

test('未修改模板保留服务端内容，清空模板是显式替换而非保留', /* templateIntent 验证空输入不会重新引入旧密钥。 */ () => {
  expect(apiConfigFromEditForm({})).toMatchObject({ headers_action: 'retain', params_action: 'retain', headers: undefined, params: undefined, body: undefined });
  // cleared 是用户手动清空三个模板后的编辑草稿。
  const cleared = apiConfigFromEditForm({ api_headers_action: 'replace', api_params_action: 'replace', api_headers: '', api_params: '', api_body: '' });
  expect(cleared).toMatchObject({ headers: {}, params: {}, body: {} });
  expect(apiConfigFromEditForm({ api_headers_action: 'clear', api_params_action: 'clear' })).toMatchObject({ headers_action: 'clear', params_action: 'clear', headers: undefined, params: undefined });
});

test('查询参数修改自动切换为替换且保持其他编辑字段', /* parameterIntent 防止已回显参数改动被保留语义吞掉。 */ () => {
  // updated 是查询参数编辑后的完整草稿。
  const updated = updateAPIEditForm({ name: '积分卡', api_params_action: 'retain' }, 'params', '{"order_id":"changed"}');
  expect(updated).toMatchObject({ name: '积分卡', api_params_action: 'replace', api_params: '{"order_id":"changed"}' });
  expect(apiConfigFromEditForm(updated).params).toBe('{"order_id":"changed"}');
});
