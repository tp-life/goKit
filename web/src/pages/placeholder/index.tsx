import { Button } from 'antd';
import { Link, useLocation } from 'react-router-dom';

/** 占位页：菜单已下发但前端尚未实现对应页面（或路径不存在）时展示 */
export default function PlaceholderPage() {
  const location = useLocation();
  return (
    <div
      style={{
        minHeight: '60vh',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 12,
      }}
    >
      <div
        className="glow-green"
        style={{ fontFamily: 'Orbitron, monospace', fontSize: 56, fontWeight: 700, color: 'var(--primary-green)' }}
      >
        404
      </div>
      <p style={{ color: 'var(--text-secondary)', margin: 0 }}>
        页面 <code style={{ color: 'var(--accent-cyan)' }}>{location.pathname}</code> 尚未接入，或没有访问权限
      </p>
      <Link to="/dashboard">
        <Button type="primary">返回工作台</Button>
      </Link>
    </div>
  );
}
