import type { ReactNode } from 'react';
import {
  ApartmentOutlined,
  DashboardOutlined,
  FileTextOutlined,
  MenuOutlined,
  UserOutlined,
  UserSwitchOutlined,
} from '@ant-design/icons';
import DashboardPage from './pages/dashboard';
import UsersPage from './pages/users';
import RolesPage from './pages/roles';
import DeptsPage from './pages/depts';
import MenusPage from './pages/menus';
import LogsPage from './pages/logs';

/**
 * 已实现的页面路由表。路径与后端菜单（sys_menus.path）一一对应：
 * 侧边栏按后端菜单树动态渲染，仅当菜单 path 在此表中注册时才可点击进入对应页面，
 * 未注册的 path 会落到占位页。
 */
export interface AdminPage {
  path: string;
  icon: ReactNode;
  element: ReactNode;
}

export const adminPages: AdminPage[] = [
  { path: '/dashboard', icon: <DashboardOutlined />, element: <DashboardPage /> },
  { path: '/system/users', icon: <UserOutlined />, element: <UsersPage /> },
  { path: '/system/roles', icon: <UserSwitchOutlined />, element: <RolesPage /> },
  { path: '/system/depts', icon: <ApartmentOutlined />, element: <DeptsPage /> },
  { path: '/system/menus', icon: <MenuOutlined />, element: <MenusPage /> },
  { path: '/system/logs', icon: <FileTextOutlined />, element: <LogsPage /> },
];

export const adminPageIcons = new Map(adminPages.map((p) => [p.path, p.icon]));
