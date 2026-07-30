import dayjs from 'dayjs';

export function fmtTime(v?: string): string {
  if (!v) return '—';
  const d = dayjs(v);
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm') : v;
}

export function fmtLatency(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`;
  return `${ms} ms`;
}

export const DATA_SCOPE_TEXT: Record<number, string> = {
  1: '全部数据',
  2: '自定义部门',
  3: '本部门及以下',
  4: '本部门',
  5: '仅本人',
};

export const MENU_TYPE_TEXT: Record<number, string> = {
  1: '目录',
  2: '菜单',
  3: '按钮',
};
