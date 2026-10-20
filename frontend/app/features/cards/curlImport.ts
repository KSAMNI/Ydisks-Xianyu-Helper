// CurlAPIConfig 保存从 cURL 命令导入并可直接写入 API 卡密表单的非持久化配置。
export interface CurlAPIConfig {
  // url 是移除查询串后的远端 API 地址，查询串会单独写入 params。
  url: string;
  // method 是当前卡密 API 支持的两种请求方法之一。
  method: 'GET' | 'POST';
  // headers 是除 Content-Type 外的请求头键值对象 JSON 文本。
  headers: string;
  // params 是 URL 查询参数键值对象 JSON 文本。
  params: string;
  // contentType 是与编辑器选项一致的正文媒体类型。
  contentType: string;
  // body 是 JSON 正文原文，或表单键值对象 JSON 文本。
  body: string;
}

// CurlImportError 标记无法无损映射到当前卡密 API 配置的 cURL 命令。
export class CurlImportError extends Error {
  // constructor 创建供界面直接展示的中文导入失败原因。
  constructor(message: string) {
    super(message);
    this.name = 'CurlImportError';
  }
}

// supportedContentTypes 保存编辑器可安全发送的正文媒体类型，避免导入后留下不可选择的值。
const supportedContentTypes = new Set([
  'application/json',
  'application/x-www-form-urlencoded',
  'text/plain',
  'application/xml',
]);

// ignoredOptionsWithValue 保存不改变当前 API 卡密语义、但会占用下一个命令行参数的常见 cURL 选项。
const ignoredOptionsWithValue = new Set([
  '--connect-timeout', '--max-time', '--retry', '--user-agent', '--cookie', '--referer', '--proxy', '--user', '--output', '--request-target',
  '-m', '-A', '-b', '-e', '-o', '-u', '-x',
]);

// tokenizeCurl 按 shell 的单引号、双引号和反斜杠规则拆分 cURL 文本，但不会执行变量展开或命令替换。
const tokenizeCurl = (command: string): string[] => {
  // tokens 保存已完成的命令行参数。
  const tokens: string[] = [];
  // current 保存正在读取的单个命令行参数。
  let current = '';
  // quote 保存当前所在的引号类型，null 表示不在引号内。
  let quote: 'single' | 'double' | null = null;
  // hasCurrent 表示即使参数为空字符串也应写入 tokens。
  let hasCurrent = false;

  for (let characterIndex = 0 /* characterIndex 表示当前检查的命令文本位置。 */; characterIndex < command.length; characterIndex += 1) {
    // character 是当前命令字符。
    const character = command[characterIndex];
    if (character === '\\' && quote !== 'single') {
      // nextCharacter 保存反斜杠后的字符，以支持换行续写和引号内转义。
      const nextCharacter = command[characterIndex + 1];
      if (nextCharacter === '\n') {
        characterIndex += 1;
        continue;
      }
      if (nextCharacter !== undefined) {
        current += nextCharacter;
        hasCurrent = true;
        characterIndex += 1;
        continue;
      }
    }
    if (character === "'" && quote !== 'double') {
      quote = quote === 'single' ? null : 'single';
      hasCurrent = true;
      continue;
    }
    if (character === '"' && quote !== 'single') {
      quote = quote === 'double' ? null : 'double';
      hasCurrent = true;
      continue;
    }
    if (/\s/.test(character) && quote === null) {
      if (hasCurrent) {
        tokens.push(current);
        current = '';
        hasCurrent = false;
      }
      continue;
    }
    current += character;
    hasCurrent = true;
  }
  if (quote !== null) throw new CurlImportError('cURL 命令的引号没有闭合');
  if (hasCurrent) tokens.push(current);
  return tokens;
};

// parseHeader 将 cURL 的单个 Header 文本拆成名称和值，并拒绝缺少冒号的无效请求头。
const parseHeader = (rawHeader: string): [string, string] => {
  // separatorIndex 是第一个冒号位置，值中其余冒号属于 Header 值本身。
  const separatorIndex = rawHeader.indexOf(':');
  if (separatorIndex <= 0) throw new CurlImportError('cURL 请求头必须使用“名称: 值”格式');
  // name 是清理空白后的 Header 名称。
  const name = rawHeader.slice(0, separatorIndex).trim();
  // value 是清理空白后的 Header 值，允许其中保留冒号。
  const value = rawHeader.slice(separatorIndex + 1).trim();
  if (!name) throw new CurlImportError('cURL 请求头名称不能为空');
  return [name, value];
};

// normalizeContentType 将带 charset 等参数的媒体类型归并为编辑器可选择的基础类型。
const normalizeContentType = (rawContentType: string | undefined, data: string[]): string => {
  // contentType 保存忽略参数和大小写后的媒体类型。
  const contentType = rawContentType?.split(';', 1)[0].trim().toLowerCase();
  if (contentType && supportedContentTypes.has(contentType)) return contentType;
  if (contentType) throw new CurlImportError(`暂不支持导入 Content-Type：${contentType}`);
  // firstData 保存首段正文，用于在未声明 Content-Type 时识别 JSON。
  const firstData = data[0]?.trim();
  if (firstData?.startsWith('{')) return 'application/json';
  return 'application/x-www-form-urlencoded';
};

// objectFromSearchParams 把 URL 或表单编码转换为当前后端接受的单值对象；同名参数保留最后一个值。
const objectFromSearchParams = (searchParams: URLSearchParams): Record<string, string> => {
  // values 保存准备序列化为表单配置的参数键值。
  const values: Record<string, string> = {};
  for (const /* entry 保存当前 URL 编码参数的名称和值。 */ entry of searchParams.entries()) {
    // key、value 分别是当前参数名称和值。
    const [key, value] = entry;
    values[key] = value;
  }
  return values;
};

// parseFormData 将 cURL 的多段 -d 表单正文合并为后端可发送的键值对象。
const parseFormData = (data: string[]): string => {
  // serialized 保存按 cURL 默认 & 语义拼接后的表单编码文本。
  const serialized = data.join('&');
  // values 保存解析后的表单字段。
  const values = objectFromSearchParams(new URLSearchParams(serialized));
  return JSON.stringify(values);
};

// parseJSONBody 校验 cURL 正文为对象，避免服务端只接受对象时导入无效数组或标量。
const parseJSONBody = (data: string[]): string => {
  // serialized 保存按原始顺序拼接的 JSON 文本；多段 JSON 不能安全映射为一个请求对象。
  const serialized = data.join('');
  // parsed 保存 JSON 解析后的未知值。
  let parsed: unknown;
  try {
    parsed = JSON.parse(serialized);
  } catch {
    throw new CurlImportError('JSON 请求正文格式无效');
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new CurlImportError('API 卡密仅支持 JSON 对象正文');
  return JSON.stringify(parsed, null, 2);
};

// consumeOptionValue 读取选项携带的下一个参数，缺失时给出面向粘贴内容的错误。
const consumeOptionValue = (tokens: string[], index: number, option: string): string => {
  // value 保存当前选项后的参数文本。
  const value = tokens[index + 1];
  if (value === undefined) throw new CurlImportError(`${option} 缺少参数`);
  return value;
};

// parseCurlCommand 将常见 cURL 请求映射为 API 卡密编辑器能够安全保存和发送的字段。
export const parseCurlCommand = (command: string): CurlAPIConfig => {
  // tokens 保存不执行 shell 的 cURL 参数序列。
  const tokens = tokenizeCurl(command.trim());
  // executable 是 cURL 可执行文件名，兼容 Windows 的 curl.exe。
  const executable = tokens.shift()?.toLowerCase();
  if (executable !== 'curl' && executable !== 'curl.exe') throw new CurlImportError('请输入以 curl 开头的命令');
  // headers 保存从 cURL 导入的非 Content-Type 请求头。
  const headers: Record<string, string> = {};
  // data 保存每个 --data 参数的原始正文片段。
  const data: string[] = [];
  // rawURL 保存命令中的远端地址，--url 与裸 URL 均可提供。
  let rawURL: string | undefined;
  // rawMethod 保存 -X/--request 指定的 HTTP 方法。
  let rawMethod: string | undefined;
  // rawContentType 保存 Header 提供的正文媒体类型。
  let rawContentType: string | undefined;
  // appendDataToURL 表示 -G/--get 要把 --data 写入查询串。
  let appendDataToURL = false;

  for (let tokenIndex = 0 /* tokenIndex 表示当前解释的 cURL 参数位置。 */; tokenIndex < tokens.length; tokenIndex += 1) {
    // token 保存当前 cURL 参数。
    const token = tokens[tokenIndex];
    // option、inlineValue 分别是 --name=value 形式的选项和值。
    const [option, inlineValue] = token.startsWith('--') ? token.split(/=(.*)/s, 2) : [token, undefined];
    // value 读取内联值或下一个参数；内联形式不会移动 tokenIndex。
    const value = (name: string): string => inlineValue ?? consumeOptionValue(tokens, tokenIndex, name);
    // usesNextValue 表示当前选项采用独立参数值，循环结束后需跳过该参数。
    const usesNextValue = inlineValue === undefined;

    if (option === '-X' || option === '--request' || option.startsWith('-X')) {
      rawMethod = option.startsWith('-X') && option.length > 2 ? option.slice(2) : value(option);
      if (usesNextValue && option.length === 2) tokenIndex += 1;
      continue;
    }
    if (option === '-H' || option === '--header' || option.startsWith('-H')) {
      // rawHeader 保存当前 cURL Header 原文。
      const rawHeader = option.startsWith('-H') && option.length > 2 ? option.slice(2) : value(option);
      // name、headerValue 分别是规范化后的 Header 名称和值。
      const [name, headerValue] = parseHeader(rawHeader);
      if (name.toLowerCase() === 'content-type') rawContentType = headerValue;
      else headers[name] = headerValue;
      if (usesNextValue && option.length === 2) tokenIndex += 1;
      continue;
    }
    if (option === '-d' || option === '--data' || option === '--data-raw' || option === '--data-binary' || option === '--data-ascii' || option === '--data-urlencode' || option.startsWith('-d')) {
      data.push(option.startsWith('-d') && option.length > 2 ? option.slice(2) : value(option));
      if (usesNextValue && option.length === 2) tokenIndex += 1;
      continue;
    }
    if (option === '--url') {
      rawURL = value(option);
      if (usesNextValue) tokenIndex += 1;
      continue;
    }
    if (option === '-G' || option === '--get') {
      appendDataToURL = true;
      continue;
    }
    if (option === '-F' || option === '--form' || option === '--form-string') throw new CurlImportError('暂不支持导入 multipart/form-data；请手动填写接口配置');
    if (ignoredOptionsWithValue.has(option)) {
      if (usesNextValue) tokenIndex += 1;
      continue;
    }
    if (option.startsWith('-')) continue;
    if (!rawURL) rawURL = token;
  }
  if (!rawURL) throw new CurlImportError('cURL 命令缺少 API 地址');
  // requestURL 保存经标准 URL 解析后的地址，避免把查询参数误填到 URL 输入框。
  let requestURL: URL;
  try {
    requestURL = new URL(rawURL);
  } catch {
    throw new CurlImportError('cURL 中的 API 地址无效');
  }
  // params 保存地址中的查询参数，-G 时会合并表单正文。
  const params = objectFromSearchParams(requestURL.searchParams);
  if (appendDataToURL && data.length > 0) Object.assign(params, objectFromSearchParams(new URLSearchParams(data.join('&'))));
  requestURL.search = '';
  // method 保存 cURL 隐式或显式指定、且受当前 API 卡密支持的方法。
  const method = (rawMethod ?? (data.length > 0 && !appendDataToURL ? 'POST' : 'GET')).toUpperCase();
  if (method !== 'GET' && method !== 'POST') throw new CurlImportError(`当前 API 卡密只支持 GET 或 POST，不能导入 ${method}`);
  if (method === 'GET' && data.length > 0 && !appendDataToURL) throw new CurlImportError('GET 请求中的 -d 正文无法发送；请在 cURL 中使用 -G 把它转为查询参数');
  // contentType 保存可由当前下拉框选择的正文媒体类型。
  const contentType = normalizeContentType(rawContentType, data);
  if (data.length > 0 && (contentType === 'text/plain' || contentType === 'application/xml')) {
    throw new CurlImportError(`${contentType} 的原始 cURL 正文不能无损导入；请手动填写 Body 字段`);
  }
  // body 保存符合当前正文编辑器和后端契约的配置文本。
  const body = appendDataToURL || data.length === 0 ? '' : contentType === 'application/json' ? parseJSONBody(data) : parseFormData(data);
  return {
    url: requestURL.toString(),
    method,
    headers: JSON.stringify(headers),
    params: JSON.stringify(params),
    contentType,
    body,
  };
};
