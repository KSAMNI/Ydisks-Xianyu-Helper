// RE2_STANDALONE_FLAGS 匹配 Go/RE2 允许、但浏览器 RegExp 不支持的独立内联标志组，如 (?i)、(?ms)、(?U)；
// 这些标志只改变匹配语义，不影响语法是否成立，因此预检时直接移除。
const RE2_STANDALONE_FLAGS = /\(\?[imsU]+\)/g;

// RE2_NAMED_GROUP 匹配 Go/RE2 的命名捕获组写法 (?P<name>，浏览器对应写法是 (?<name>。
const RE2_NAMED_GROUP = /\(\?P</g;

// RE2_SCOPED_FLAGS 匹配浏览器不支持的局部标志组前缀，如 (?i:、(?-i:；语法检查时统一降级为非捕获分组。
const RE2_SCOPED_FLAGS = /\(\?[-imsU]+:/g;

/**
 * precheckRegexpExpression 在提交前对 Go/RE2 正则做尽力而为的语法预检。
 * 后端仍是唯一权威：这里只拦截明显的语法错误以便即时提示，因此必须先把浏览器不认识、
 * 但 Go/RE2 合法的写法（独立内联标志、(?P<name>）转换成等价的浏览器写法再检查，避免把合法规则误拦在前端。
 *
 * @param expression 用户输入的单条正则表达式，已去除首尾空白。
 * @returns 转换后能被浏览器 RegExp 解析时返回 true，否则返回 false。
 */
export const precheckRegexpExpression = (expression: string): boolean => {
  // browserCompatible 是仅用于语法检查的浏览器兼容写法，不会被提交或用于真实匹配。
  const browserCompatible = expression.replace(RE2_SCOPED_FLAGS, '(?:').replace(RE2_STANDALONE_FLAGS, '').replace(RE2_NAMED_GROUP, '(?<');
  try {
    new RegExp(browserCompatible);
    return true;
  } catch {
    return false;
  }
};
