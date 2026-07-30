import React from 'react';
import ReactDOM from 'react-dom/client';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';

import '@fontsource/orbitron/500.css';
import '@fontsource/orbitron/700.css';
import '@fontsource/orbitron/900.css';
import '@fontsource/noto-sans-sc/400.css';
import '@fontsource/noto-sans-sc/500.css';
import '@fontsource/noto-sans-sc/700.css';
import '@fontsource/jetbrains-mono/400.css';
import '@fontsource/jetbrains-mono/500.css';
import '@fontsource/jetbrains-mono/700.css';

import './global.css';
import App from './App';
import { AuthProvider } from './store/auth';
import { ThemeProvider, useTheme } from './store/theme';
import { registerMessage } from './utils/notify';

dayjs.locale('zh-cn');

/** 把 antd App 上下文的 message 实例暴露给 request 层 */
function MessageBridge({ children }: { children: React.ReactNode }) {
  const { message } = AntApp.useApp();
  registerMessage(message);
  return <>{children}</>;
}

/** 按当前主题预设渲染 antd ConfigProvider */
function ThemedApp() {
  const { preset } = useTheme();
  return (
    <ConfigProvider locale={zhCN} theme={preset.antd}>
      <AntApp>
        <MessageBridge>
          <AuthProvider>
            <App />
          </AuthProvider>
        </MessageBridge>
      </AntApp>
    </ConfigProvider>
  );
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider>
      <ThemedApp />
    </ThemeProvider>
  </React.StrictMode>,
);
