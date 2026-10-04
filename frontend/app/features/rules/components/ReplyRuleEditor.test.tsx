// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { ReplyRule } from '../types';
import ReplyRuleEditor from './ReplyRuleEditor';

/** ReplyRuleEditorHarness 用真实草稿状态承接编辑器输入，保存回调只记录草稿而不触达网络。 */
const ReplyRuleEditorHarness = ({ initialRule,onSave }: {
  /** initialRule 是当前用例的旧规则或新规则初始草稿。 */
  initialRule: Partial<ReplyRule>;
  /** onSave 记录用户点击保存时的完整表单，用于验证输入没有隐式清理。 */
  onSave: (rule: Partial<ReplyRule>) => void;
}) => {
  // rule/setRule 拥有本用例的受控表单状态，关闭后卸载编辑器。
  const [rule, setRule] = useState<Partial<ReplyRule> | null>(initialRule);
  return rule ? <ReplyRuleEditor rule={rule} setRule={setRule} items={[]} onClose={/* 当前回调响应取消并移除草稿。 */ () => setRule(null)} onSave={/* 当前回调记录用户保存时的草稿。 */ () => onSave(rule)} /> : null;
};

afterEach(/* 当前回调清理编辑器 portal，避免各用例共享 DOM。 */ () => cleanup());

describe('ReplyRuleEditor', /* 当前回调验证多表达式编辑器的可访问名称和原始输入行为。 */ () => {
  test('匹配模式和每行表达式有明确可访问名称，旧规则默认包含匹配', /* 当前回调按用户可感知的标签查找控件并验证兼容默认值。 */ () => {
    render(<ReplyRuleEditorHarness initialRule={{ keyword: '旧关键词', reply_content: '已命中' }} onSave={vi.fn()} />);
    expect((screen.getByRole('combobox', { name: '匹配模式' }) as HTMLSelectElement).value).toBe('contains');
    expect((screen.getByRole('textbox', { name: '第 1 个关键词表达式' }) as HTMLInputElement).value).toBe('旧关键词');
    expect((screen.getByRole('button', { name: '删除第 1 个表达式' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/正则语法由后端 Go\/RE2 校验/)).toBeTruthy();
    expect(screen.getByText(/默认忽略大小写/)).toBeTruthy();
  });

  test('切换正则模式后可按名称新增、编辑和删除表达式，并保留空白', /* 当前回调验证多行编辑到保存的用户操作，不依赖内部状态。 */ () => {
    // onSave 记录编辑器保存动作发出的原始草稿。
    const onSave = vi.fn();
    render(<ReplyRuleEditorHarness initialRule={{ keyword: '旧关键词', reply_content: '已命中' }} onSave={onSave} />);
    fireEvent.change(screen.getByRole('combobox', { name: '匹配模式' }), { target: { value: 'regexp' } });
    fireEvent.change(screen.getByRole('textbox', { name: '第 1 个关键词表达式' }), { target: { value: 'foo\\ ' } });
    fireEvent.click(screen.getByRole('button', { name: '添加表达式' }));
    fireEvent.change(screen.getByRole('textbox', { name: '第 2 个关键词表达式' }), { target: { value: '   ' } });
    fireEvent.click(screen.getByRole('button', { name: '保存规则' }));
    expect(onSave).toHaveBeenLastCalledWith(expect.objectContaining({ keyword: 'foo\\ ', expressions: ['foo\\ ', '   '], match_type: 'regexp' }));

    fireEvent.click(screen.getByRole('button', { name: '删除第 1 个表达式' }));
    expect((screen.getByRole('textbox', { name: '第 1 个关键词表达式' }) as HTMLInputElement).value).toBe('   ');
    expect(screen.queryByRole('textbox', { name: '第 2 个关键词表达式' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '保存规则' }));
    expect(onSave).toHaveBeenLastCalledWith(expect.objectContaining({ keyword: '   ', expressions: ['   '], match_type: 'regexp' }));
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(screen.queryByRole('combobox', { name: '匹配模式' })).toBeNull();
  });
});
