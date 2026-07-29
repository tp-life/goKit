interface StatusPillProps {
  ok: boolean;
  okText?: string;
  offText?: string;
}

/** 状态胶囊：左侧圆点 + 文字，扁平现代风格 */
export default function StatusPill({ ok, okText = '启用', offText = '禁用' }: StatusPillProps) {
  return (
    <span className={`status-pill ${ok ? 'status-pill-ok' : 'status-pill-off'}`}>
      <span className="status-pill-dot" />
      {ok ? okText : offText}
    </span>
  );
}
