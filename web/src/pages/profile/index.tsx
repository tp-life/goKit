import { useState } from 'react';
import type { CSSProperties } from 'react';
import { App, Card, Col, Descriptions, Row, Tag } from 'antd';
import { ProFormText, ProForm } from '@ant-design/pro-components';
import { changePassword } from '../../api/auth';
import { useAuth } from '../../store/auth';
import StatusPill from '../../components/StatusPill';

export default function ProfilePage() {
  const { message } = App.useApp();
  const { profile } = useAuth();
  const [submitting, setSubmitting] = useState(false);

  return (
    <div>
      <div className="gk-page-head fade-up">
        <div className="gk-page-kicker">Account · Profile</div>
        <h2 className="gk-page-title">个人中心</h2>
      </div>

      <Row gutter={[20, 20]}>
        <Col xs={24} lg={12}>
          <Card title="我的资料" className="fade-up" style={{ '--d': '120ms' } as CSSProperties}>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="用户 ID">
                <span className="mono">{profile?.id}</span>
              </Descriptions.Item>
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
            </Descriptions>
            <div style={{ marginTop: 12 }}>
              <div style={{ marginBottom: 6, color: 'var(--text-secondary)', fontSize: 12 }}>
                权限点（{profile?.perms?.length ?? 0}）
              </div>
              <div className="profile-perm-tags">
                {profile?.perms?.length
                  ? profile.perms.map((p) => (
                      <Tag key={p} className="mono" style={{ marginBottom: 4 }}>
                        {p}
                      </Tag>
                    ))
                  : '—'}
              </div>
            </div>
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card title="修改密码" className="fade-up" style={{ '--d': '220ms' } as CSSProperties}>
            <ProForm
              submitter={{
                searchConfig: { submitText: '保存新密码' },
                resetButtonProps: { style: { display: 'none' } },
              }}
              loading={submitting}
              onFinish={async (values) => {
                setSubmitting(true);
                try {
                  await changePassword({
                    old_password: values.old_password,
                    new_password: values.new_password,
                  });
                  message.success('密码已修改');
                  return true;
                } catch {
                  return false;
                } finally {
                  setSubmitting(false);
                }
              }}
            >
              <ProFormText.Password
                name="old_password"
                label="旧密码"
                rules={[{ required: true, message: '请输入旧密码' }]}
              />
              <ProFormText.Password
                name="new_password"
                label="新密码"
                rules={[
                  { required: true, message: '请输入新密码' },
                  { min: 8, message: '新密码最少 8 位' },
                ]}
              />
              <ProFormText.Password
                name="confirm"
                label="确认新密码"
                dependencies={['new_password']}
                rules={[
                  { required: true, message: '请再次输入新密码' },
                  ({ getFieldValue }) => ({
                    validator: (_, v) =>
                      !v || v === getFieldValue('new_password')
                        ? Promise.resolve()
                        : Promise.reject(new Error('两次输入的密码不一致')),
                  }),
                ]}
              />
            </ProForm>
          </Card>
        </Col>
      </Row>
    </div>
  );
}
