import { describe, expect, test } from 'vitest';
import { precheckRegexpExpression } from './regexpPrecheck';

describe('precheckRegexpExpression', /* 当前回调验证前端正则预检不会误拦 Go/RE2 合法语法，同时仍拦截真正的语法错误。 */ () => {
  test.each([
    ['普通锚点和分支', '^(foo|bar)$'],
    ['量词', '^a{2,3}$'],
    ['浏览器与 RE2 都支持的局部标志组', '(?-i:foo)'],
    ['RE2 独立不区分大小写标志', '(?i)foo'],
    ['RE2 独立多行标志', '(?m)^foo$'],
    ['RE2 独立点号匹配换行标志', '(?s).'],
    ['RE2 组合内联标志', '(?ms)^a.b$'],
    ['RE2 非贪婪标志', '(?U)a+'],
    ['RE2 命名捕获组', '(?P<name>订单)\\d+'],
    ['中文与 Unicode 属性', '\\pL+'],
  ])('合法写法不被拦截：%s', /* label 是用例说明；expression 是应通过预检的正则。 */ (_label, expression) => {
    expect(precheckRegexpExpression(expression)).toBe(true);
  });

  test.each([
    ['未闭合字符类', '['],
    ['未闭合分组', '(abc'],
    ['量词前无内容', '*abc'],
    ['不完整的转义', 'abc\\'],
  ])('真正的语法错误仍被拦截：%s', /* label 是用例说明；expression 是应被拒绝的正则。 */ (_label, expression) => {
    expect(precheckRegexpExpression(expression)).toBe(false);
  });

  test('只转换预检副本，不改变调用方传入的表达式', /* 当前回调确认函数没有副作用且可重复调用。 */ () => {
    // expression 是含 RE2 独立标志的表达式。
    const expression = '(?i)foo';
    expect(precheckRegexpExpression(expression)).toBe(true);
    expect(precheckRegexpExpression(expression)).toBe(true);
    expect(expression).toBe('(?i)foo');
  });
});
