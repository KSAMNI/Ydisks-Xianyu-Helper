// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import React from 'react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { AccountDetail } from '../api';
import type { AccountEditForm,AccountEditModalProps } from '../types';
import { AccountEditModal } from './AccountEditModal';

// baseForm 是编辑弹窗测试使用的最小非敏感表单，三个开关默认全部关闭。
const baseForm: AccountEditForm = {
  remark: '',
  cookie: '',
  auto_confirm: false,
  auto_consign: false,
  auto_bargain: false,
  pause_duration: 0,
  username: '',
  login_password: '',
  show_browser: false,
  showLoginPassword: false,
  clear_password: false,
};

// Harness 用真实 useState 承载表单，验证开关点击后受控状态与 aria-checked 同步变化。
const Harness: React.FC<{
  // initial 覆盖默认表单的初始字段。
  initial?: Partial<AccountEditForm>;
  // onForm 在每次渲染时接收最新表单快照。
  onForm?: (form: AccountEditForm) => void;
}> = ({ initial, onForm }) => {
  // editForm、setEditForm 是弹窗受控的编辑表单状态及其更新函数。
  const [editForm, setEditForm] = React.useState<AccountEditForm>({ ...baseForm, ...initial });
  onForm?.(editForm);
  // props 是弹窗所需的最小完整属性集合，回调均为空实现。
  const props = {
    account: { id: 'account-1', paused: false, pause_duration: 0 } as AccountDetail,
    editForm,
    setEditForm,
    saving: false,
    onClose: vi.fn(),
    onSave: vi.fn(),
    onRestartPause: vi.fn(),
    longLogin: { loading: false, saving: false, canOpen: false, enabled: false, error: '' },
    onToggleLongLogin: vi.fn(),
    passwordLoginView: { sessionId: '', status: 'idle', message: '', qrCodeUrl: '' },
    onPasswordLogin: vi.fn(),
    onCancelPasswordLogin: vi.fn(),
    notifChannels: [],
    selectedChannelIds: [],
    bindingsLoaded: true,
    bindingsLoading: false,
    bindingsLoadError: '',
    onRetryBindings: vi.fn(),
    onToggleChannel: vi.fn(),
    onSettingsDirty: vi.fn(),
  } as unknown as AccountEditModalProps;
  return <AccountEditModal {...props} />;
};

// switches 读取弹窗内按顺序排列的三个业务开关：自动发货、自动确认发货、自动免拼。
const switches = (): HTMLElement[] => screen.getAllByRole('switch').slice(0, 3);

describe('AccountEditModal 业务开关', /* 验证三个开关互不串联且宽度不会被说明文字挤压。 */ () => {
  afterEach(/* 清理渲染的弹窗，避免门户内容污染后续用例。 */ () => {
    cleanup();
  });

  test('开关以 role=switch 暴露状态，点击只切换对应字段', /* 自动确认发货只影响 auto_consign。 */ () => {
    // latest 保存最近一次渲染得到的表单，用于断言字段独立性。
    let latest = baseForm;
    render(<Harness onForm={/* 记录最新表单快照。 */ form => { latest = form; }} />);
    expect(switches().map(/* 读取每个开关的选中状态。 */ item => item.getAttribute('aria-checked'))).toEqual(['false', 'false', 'false']);
    fireEvent.click(switches()[1]);
    expect(switches()[1].getAttribute('aria-checked')).toBe('true');
    expect(latest).toMatchObject({ auto_confirm: false, auto_consign: true, auto_bargain: false });
    fireEvent.click(switches()[1]);
    expect(latest.auto_consign).toBe(false);
  });

  test('自动发货与自动免拼各自独立切换', /* 点击第一和第三个开关只改变各自字段。 */ () => {
    // latest 保存最近一次渲染得到的表单。
    let latest = baseForm;
    render(<Harness onForm={/* 记录最新表单快照。 */ form => { latest = form; }} />);
    fireEvent.click(switches()[0]);
    fireEvent.click(switches()[2]);
    expect(latest).toMatchObject({ auto_confirm: true, auto_consign: false, auto_bargain: true });
  });

  test('激活状态的开关不被压缩且圆点处于右侧位置', /* shrink-0 与 min-w-14 防止长说明挤压轨道。 */ () => {
    render(<Harness initial={{ auto_confirm: true, auto_consign: true, auto_bargain: true }} />);
    for (const item /* item 是当前被检查的业务开关按钮。 */ of switches()) {
      expect(item.getAttribute('aria-checked')).toBe('true');
      expect(item.className).toContain('shrink-0');
      expect(item.className).toContain('min-w-14');
      expect(item.className).toContain('bg-brand');
      expect(item.querySelector('span')?.className).toContain('translate-x-6');
    }
  });
});
