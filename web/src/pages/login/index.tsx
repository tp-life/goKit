import { Button, Form, Input } from 'antd';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../../store/auth';
import { useTheme } from '../../store/theme';
import MatrixRain from '../../components/MatrixRain';

interface LoginForm {
  username: string;
  password: string;
}

export default function LoginPage() {
  const { login } = useAuth();
  const { preset } = useTheme();
  const navigate = useNavigate();
  const [submitting, setSubmitting] = useState(false);

  const onFinish = async (values: LoginForm) => {
    setSubmitting(true);
    try {
      await login(values.username, values.password);
      navigate('/dashboard', { replace: true });
    } catch {
      // 错误提示由 request 层统一处理
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="login-wrap">
      <div className="login-left">
        {preset.effects.rain && (
          <MatrixRain
            className="login-rain"
            color={preset.antd.token?.colorPrimary as string}
            headColor={preset.antd.token?.colorText as string}
            bg={preset.antd.token?.colorBgLayout as string}
          />
        )}
        <div className="login-issue">GoKit · Admin Console</div>
        <h1 className="login-masthead">
          GoKit
          <span className="login-masthead-dot" />
        </h1>
        <div className="login-rule" />
        <p className="login-sub">System Administration</p>
        <p className="login-desc">
          统一管理用户、角色、部门、菜单与操作日志的后台控制台。
        </p>
      </div>
      <div className="login-right">
        <div className="login-card fade-up">
          <h2>登录</h2>
          <p className="login-hint">Sign in to the console</p>
          <Form<LoginForm> layout="vertical" size="large" onFinish={onFinish} requiredMark={false}>
            <Form.Item
              label="用户名"
              name="username"
              rules={[{ required: true, message: '请输入用户名' }]}
            >
              <Input prefix={<UserOutlined />} placeholder="username" autoComplete="username" />
            </Form.Item>
            <Form.Item
              label="密码"
              name="password"
              rules={[{ required: true, message: '请输入密码' }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                placeholder="password"
                autoComplete="current-password"
              />
            </Form.Item>
            <Form.Item style={{ marginTop: 8 }}>
              <Button
                type="primary"
                htmlType="submit"
                block
                loading={submitting}
                className="login-btn"
              >
                登 录
              </Button>
            </Form.Item>
          </Form>
          <div className="login-footer">GoKit Admin Console</div>
        </div>
      </div>
    </div>
  );
}
