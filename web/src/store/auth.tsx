import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import * as authApi from '../api/auth';
import { clearToken, getToken, setToken } from '../api/request';
import type { Profile } from '../api/types';

interface AuthContextValue {
  token: string | null;
  profile: Profile | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
  refreshProfile: () => Promise<void>;
  hasPerm: (perm: string) => boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(getToken());
  const [profile, setProfile] = useState<Profile | null>(null);
  const [loading, setLoading] = useState<boolean>(!!getToken());

  const refreshProfile = useCallback(async () => {
    const p = await authApi.getProfile();
    setProfile(p);
  }, []);

  useEffect(() => {
    if (!token) {
      setLoading(false);
      return;
    }
    setLoading(true);
    authApi
      .getProfile()
      .then(setProfile)
      .catch(() => {
        // 401 时 request 层已清 token 并跳转，这里同步本地状态
        setTokenState(getToken());
      })
      .finally(() => setLoading(false));
  }, [token]);

  const login = useCallback(
    async (username: string, password: string) => {
      const data = await authApi.login({ username, password });
      setToken(data.token);
      setTokenState(data.token);
      const p = await authApi.getProfile();
      setProfile(p);
    },
    [],
  );

  const logout = useCallback(() => {
    clearToken();
    setTokenState(null);
    setProfile(null);
  }, []);

  const hasPerm = useCallback(
    (perm: string) => {
      if (!profile) return false;
      if (profile.is_super) return true;
      return (profile.perms ?? []).includes(perm);
    },
    [profile],
  );

  const value = useMemo(
    () => ({ token, profile, loading, login, logout, refreshProfile, hasPerm }),
    [token, profile, loading, login, logout, refreshProfile, hasPerm],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth 必须在 AuthProvider 内使用');
  return ctx;
}

/** 按钮级权限控制：const { hasPerm } = usePerms(); hasPerm('system:user:create') */
export function usePerms() {
  const { hasPerm } = useAuth();
  return { hasPerm };
}
