import type { CardAPIConfigInput } from './models';
import type { AddCardForm, EditCardForm } from './types';

// apiConfigFromEditForm 让保存和测试使用相同的草稿与保留语义，避免测试丢失已保存密钥。
export const apiConfigFromEditForm = (form: EditCardForm): CardAPIConfigInput => ({
  url: form.api_url?.trim() || '',
  method: form.api_method || 'GET',
  timeout_seconds: form.api_timeout || 10,
  headers: form.api_headers_action === 'clear' ? undefined : form.api_headers_action === 'replace' ? form.api_headers?.trim() || {} : undefined,
  params: form.api_params_action === 'clear' ? undefined : form.api_params_action === 'replace' ? form.api_params?.trim() || {} : undefined,
  content_type: form.api_content_type || 'application/json',
  body: form.api_body === undefined ? undefined : form.api_body.trim() || {},
  headers_action: form.api_headers_action || 'retain',
  params_action: form.api_params_action || 'retain',
  response_path: form.api_response_path?.trim() || '',
  retry_enabled: form.api_retry_enabled || false,
});

// apiConfigFromAddForm 新建时两个模板均明确替换，测试与创建复用同一配置映射。
export const apiConfigFromAddForm = (form: AddCardForm): CardAPIConfigInput => apiConfigFromEditForm({
  ...form, api_url: form.content, api_headers_action: 'replace', api_params_action: 'replace',
});

// apiFormFields 将请求编辑器字段映射到表单，避免新建与编辑重复维护条件分支。
const apiFormFields = {
  url: 'api_url', method: 'api_method', timeout: 'api_timeout', headers: 'api_headers', params: 'api_params',
  contentType: 'api_content_type', body: 'api_body', responsePath: 'api_response_path', retryEnabled: 'api_retry_enabled',
} as const;

// updateAPIEditForm 将编辑器的已类型化值写回草稿，修改模板时自动采用替换语义。
export const updateAPIEditForm = (form: EditCardForm, field: keyof typeof apiFormFields, value: string | number | boolean): EditCardForm => ({
  ...form, [apiFormFields[field]]: value,
  ...(field === 'headers' ? { api_headers_action: 'replace' as const } : {}),
  ...(field === 'params' ? { api_params_action: 'replace' as const } : {}),
});

// updateAPIAddForm 将同一编辑器字段写回新建表单，新建地址使用 content 字段。
export const updateAPIAddForm = (form: AddCardForm, field: keyof typeof apiFormFields, value: string | number | boolean): AddCardForm => ({
  ...form, [field === 'url' ? 'content' : apiFormFields[field]]: value,
});
