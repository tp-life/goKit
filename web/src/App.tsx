import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import type { ReactElement } from 'react';
import { useAuth } from './store/auth';
import { adminPages } from './routes';
import AdminLayout from './layouts/AdminLayout';
import LoginPage from './pages/login';
import ProfilePage from './pages/profile';
import PlaceholderPage from './pages/placeholder';

function RequireAuth({ children }: { children: ReactElement }) {
  const { token } = useAuth();
  if (!token) return <Navigate to="/login" replace />;
  return children;
}

/** 旧版静态路径 → 新路径（与后端菜单 sys_menus.path 对齐） */
const legacyRedirects: [string, string][] = [
  ['users', '/system/users'],
  ['roles', '/system/roles'],
  ['depts', '/system/depts'],
  ['menus', '/system/menus'],
  ['logs', '/system/logs'],
];

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <AdminLayout />
            </RequireAuth>
          }
        >
          <Route index element={<Navigate to="/dashboard" replace />} />
          {adminPages.map((p) => (
            <Route key={p.path} path={p.path.slice(1)} element={p.element} />
          ))}
          <Route path="profile" element={<ProfilePage />} />
          {legacyRedirects.map(([from, to]) => (
            <Route key={from} path={from} element={<Navigate to={to} replace />} />
          ))}
          <Route path="*" element={<PlaceholderPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
