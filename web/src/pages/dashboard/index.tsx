import { Card, Col, Descriptions, Row, Tag } from 'antd';
import {
  ApartmentOutlined,
  FileTextOutlined,
  MenuOutlined,
  UserOutlined,
  UserSwitchOutlined,
} from '@ant-design/icons';
import dayjs from 'dayjs';
import { useNavigate } from 'react-router-dom';
import type { CSSProperties, ReactNode } from 'react';
import { useAuth } from '../../store/auth';
import StatusPill from '../../components/StatusPill';

const entries: { title: string; desc: string; path: string; icon: ReactNode }[] = [
  { title: '用户管理', desc: '账号、角色与密码', path: '/system/users', icon: <UserOutlined /> },
  { title: '角色管理', desc: '权限点与数据范围', path: '/system/roles', icon: <UserSwitchOutlined /> },
  { title: '部门管理', desc: '组织架构树', path: '/system/depts', icon: <ApartmentOutlined /> },
  { title: '菜单管理', desc: '目录、菜单与按钮', path: '/system/menus', icon: <MenuOutlined /> },
  { title: '操作日志', desc: '审计与追溯', path: '/system/logs', icon: <FileTextOutlined /> },
];

export default function DashboardPage() {
  const { profile } = useAuth();
  const navigate = useNavigate();

  return (
    <div>
      <div className="hero fade-up">
        <div className="hero-kicker">
          GoKit Console — {dayjs().format('YYYY年MM月DD日 dddd')}
        </div>
        <h1 className="hero-title">你好，{profile?.nickname || profile?.username || '编辑'}</h1>
        <p className="hero-sub">欢迎回来。今天的系统运转正常。</p>
      </div>

      <Row gutter={[20, 20]}>
        <Col xs={24} lg={10}>
          <Card
            title="当前用户"
            className="fade-up"
            style={{ '--d': '120ms', height: '100%' } as CSSProperties}
          >
            <Descriptions column={1} size="small">
              <Descriptions.Item label="用户名">
                <span className="mono">{profile?.username}</span>
              </Descriptions.Item>
              <Descriptions.Item label="昵称">{profile?.nickname || '—'}</Descriptions.Item>
              <Descriptions.Item label="邮箱">{profile?.email || '—'}</Descriptions.Item>
              <Descriptions.Item label="部门 ID">
                <span className="mono">{profile?.dept_id ?? '—'}</span>
              </Descriptions.Item>
              <Descriptions.Item label="身份">
                {profile?.is_super ? (
                  <StatusPill ok okText="超级管理员" />
                ) : (
                  <StatusPill ok={false} offText="普通用户" />
                )}
              </Descriptions.Item>
              <Descriptions.Item label="角色">
                {profile?.roles?.length
                  ? profile.roles.map((r) => (
                      <Tag key={r} className="mono" style={{ marginInlineEnd: 4 }}>
                        {r}
                      </Tag>
                    ))
                  : '—'}
              </Descriptions.Item>
              <Descriptions.Item label="权限点">
                <span className="mono">{profile?.perms?.length ?? 0}</span> 个
              </Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
        <Col xs={24} lg={14}>
          <Card title="快捷入口" className="fade-up" style={{ '--d': '220ms' } as CSSProperties}>
            <div className="quick-grid">
              {entries.map((e, i) => (
                <a
                  key={e.path}
                  className="quick-card fade-up"
                  style={{ '--d': `${260 + i * 70}ms` } as CSSProperties}
                  onClick={(ev) => {
                    ev.preventDefault();
                    navigate(e.path);
                  }}
                  href={e.path}
                >
                  <div className="quick-card-icon">{e.icon}</div>
                  <div className="quick-card-title">{e.title}</div>
                  <div className="quick-card-desc">{e.desc}</div>
                </a>
              ))}
            </div>
          </Card>
        </Col>
      </Row>
    </div>
  );
}
