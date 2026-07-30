import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { defaultThemeId, themePresets } from '../theme';
import type { ThemePreset } from '../theme';

const STORAGE_KEY = 'gk-theme';

interface ThemeContextValue {
  preset: ThemePreset;
  setTheme: (id: string) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

function readInitial(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) || defaultThemeId;
  } catch {
    return defaultThemeId;
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [themeId, setThemeId] = useState<string>(readInitial);
  const preset = themePresets.find((p) => p.id === themeId) ?? themePresets[0];

  useEffect(() => {
    // global.css 按 [data-theme] 提供对应主题的 CSS 变量
    document.documentElement.dataset.theme = preset.id;
    try {
      localStorage.setItem(STORAGE_KEY, preset.id);
    } catch {
      // 隐私模式等场景下忽略持久化失败
    }
  }, [preset.id]);

  const value = useMemo(() => ({ preset, setTheme: setThemeId }), [preset]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme 必须在 ThemeProvider 内使用');
  return ctx;
}
