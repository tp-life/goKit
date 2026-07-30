import { useEffect, useMemo, useState } from 'react';
import { ProLayout } from '@ant-design/pro-components';
import type { MenuDataItem } from '@ant-design/pro-components';
import { Avatar, Dropdown, Spin } from 'antd';
import {
  BgColorsOutlined,
  CheckOutlined,
  DashboardOutlined,
  FolderOutlined,
  LinkOutlined,
  LogoutOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { getMyMenus } from '../api/menu';
import type { MenuNode } from '../api/types';
import MatrixRain from '../components/MatrixRain';
import { adminPageIcons } from '../routes';
import { useAuth } from '../store/auth';
import { useTheme } from '../store/theme';
import { themePresets } from '../theme';

/** 主题切换器：顶栏色板下拉 */
function ThemeSwitcher() {
  const { preset, setTheme } = useTheme();
  return (
    <Dropdown
      trigger={['click']}
      menu={{
        selectedKeys: [preset.id],
        items: themePresets.map((t) => ({
          key: t.id,
          label: (
            <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: '50%',
                  background: t.swatch,
                  boxShadow: `0 0 6px ${t.swatch}`,
                  flex: 'none',
                }}
              />
              <span>{t.name}</span>
              <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>{t.desc}</span>
              {t.id === preset.id && <CheckOutlined style={{ marginLeft: 'auto' }} />}
            </span>
          ),
        })),
        onClick: ({ key }) => setTheme(key),
      }}
    >
      <span
        title="切换主题"
        style={{
          cursor: 'pointer',
          padding: '0 12px',
          fontSize: 16,
          color: 'var(--text-secondary)',
          display: 'inline-flex',
          alignItems: 'center',
        }}
      >
        <BgColorsOutlined />
      </span>
    </Dropdown>
  );
}

/** 后端菜单树 → ProLayout 路由项。目录仅作分组，菜单须有 path 才可点击。 */
function toMenuRoutes(nodes: MenuNode[]): MenuDataItem[] {
  const routes: MenuDataItem[] = [];
  for (const n of nodes) {
    const children = n.children?.length ? toMenuRoutes(n.children) : [];
    if (n.type === 1) {
      // 目录：没有可见子节点时不展示
      if (!children.length) continue;
      routes.push({
        path: n.path || `/dir-${n.id}`,
        name: n.title,
        icon: <FolderOutlined />,
        children,
      });
    } else if (n.type === 2 && n.path) {
      routes.push({
        path: n.path,
        name: n.title,
        icon: adminPageIcons.get(n.path) ?? <LinkOutlined />,
      });
    }
  }
  return routes;
}

export default function AdminLayout() {
  const { profile, loading, logout } = useAuth();
  const { preset } = useTheme();
  const location = useLocation();
  const navigate = useNavigate();
  const [menuTree, setMenuTree] = useState<MenuNode[] | null>(null);

  useEffect(() => {
    getMyMenus()
      .then(setMenuTree)
      .catch(() => setMenuTree([]));
  }, []);

  const menuRoutes = useMemo<MenuDataItem[]>(
    () => [
      { path: '/dashboard', name: '工作台', icon: <DashboardOutlined /> },
      ...toMenuRoutes(menuTree ?? []),
    ],
    [menuTree],
  );

  if ((loading && !profile) || menuTree === null) {
    return (
      <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Spin size="large" />
      </div>
    );
  }

  return (
    <>
      {preset.effects.rain && (
        <MatrixRain
          className="admin-rain"
          color={preset.antd.token?.colorPrimary as string}
          headColor={preset.antd.token?.colorText as string}
          bg={preset.antd.token?.colorBgLayout as string}
        />
      )}
      <ProLayout
        title={false}
        logo={false}
        layout="side"
        fixSiderbar
        fixedHeader
        siderWidth={220}
        location={{ pathname: location.pathname }}
        route={{ path: '/', routes: menuRoutes }}
        menu={{ locale: false }}
        menuItemRender={(item, dom) =>
          item.path ? <Link to={item.path}>{dom}</Link> : dom
        }
        headerTitleRender={() => (
          <div className="gk-brand">
            <span className="gk-brand-dot" />
            <span className="gk-brand-text">
              <span className="gk-brand-name">GoKit</span>
              <span className="gk-brand-sub">Admin Console</span>
            </span>
          </div>
        )}
        actionsRender={() => [<ThemeSwitcher key="theme" />]}
        token={preset.layoutToken}
        avatarProps={{
        size: 'small',
        icon: <UserOutlined />,
        title: profile?.nickname || profile?.username || '未登录',
        render: (_props, dom) => (
          <Dropdown
            menu={{
              items: [
                { key: 'profile', icon: <UserOutlined />, label: '个人中心' },
                { type: 'divider' },
                { key: 'logout', icon: <LogoutOutlined />, label: '退出登录', danger: true },
              ],
              onClick: ({ key }) => {
                if (key === 'profile') navigate('/profile');
                if (key === 'logout') {
                  logout();
                  navigate('/login', { replace: true });
                }
              },
            }}
          >
            {dom}
          </Dropdown>
        ),
      }}
        contentStyle={{ background: preset.effects.rain ? 'transparent' : 'var(--bg)' }}
      >
        <Outlet />
      </ProLayout>
    </>
  );
}
