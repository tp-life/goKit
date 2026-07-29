import { useEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import { App, Button, Modal, Popconfirm, Select, Space } from 'antd';
import {
  ModalForm,
  ProFormRadio,
  ProFormSelect,
  ProFormText,
  ProFormTreeSelect,
  ProTable,
} from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PlusOutlined } from '@ant-design/icons';
import {
  assignUserRoles,
  createUser,
  deleteUser,
  getUser,
  listUsers,
  resetUserPassword,
  updateUser,
} from '../../api/user';
import { listRoles } from '../../api/role';
import { getDeptTree } from '../../api/dept';
import type { Role, User } from '../../api/types';
import { deptTreeToSelect, flattenDepts } from '../../utils/tree';
import type { TreeSelectNode } from '../../utils/tree';
import { fmtTime } from '../../utils/format';
import { usePerms } from '../../store/auth';
import StatusPill from '../../components/StatusPill';

export default function UsersPage() {
  const { message } = App.useApp();
  const { hasPerm } = usePerms();
  const actionRef = useRef<ActionType>();

  const [deptOptions, setDeptOptions] = useState<TreeSelectNode[]>([]);
  const [deptMap, setDeptMap] = useState<Record<number, string>>({});
  const [roleOptions, setRoleOptions] = useState<{ label: string; value: number }[]>([]);

  const [createOpen, setCreateOpen] = useState(false);
  const [editUser, setEditUser] = useState<User | null>(null);
  const [roleUser, setRoleUser] = useState<User | null>(null);
  const [roleIds, setRoleIds] = useState<number[]>([]);
  const [pwdUser, setPwdUser] = useState<User | null>(null);

  useEffect(() => {
    getDeptTree()
      .then((tree) => {
        setDeptOptions(deptTreeToSelect(tree ?? []));
        setDeptMap(flattenDepts(tree ?? []));
      })
      .catch(() => {});
    listRoles({ page: 1, page_size: 100 })
      .then((d) =>
        setRoleOptions((d?.list ?? []).map((r: Role) => ({ label: `${r.name}（${r.code}）`, value: r.id }))),
      )
      .catch(() => {});
  }, []);

  const columns: ProColumns<User>[] = [
    {
      title: '关键字',
      dataIndex: 'keyword',
      hideInTable: true,
      fieldProps: { placeholder: '用户名 / 昵称' },
    },
    { title: 'ID', dataIndex: 'id', width: 72, hideInSearch: true, render: (_, r) => <span className="mono">{r.id}</span> },
    { title: '用户名', dataIndex: 'username', hideInSearch: true, render: (_, r) => <span className="mono">{r.username}</span> },
    { title: '昵称', dataIndex: 'nickname', hideInSearch: true, render: (v) => v || '—' },
    { title: '邮箱', dataIndex: 'email', hideInSearch: true, render: (v) => v || '—' },
    {
      title: '部门',
      dataIndex: 'dept_id',
      hideInSearch: true,
      render: (_, r) => deptMap[r.dept_id] ?? (r.dept_id ? `#${r.dept_id}` : '—'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      valueEnum: {
        1: { text: '启用' },
        0: { text: '禁用' },
      },
      render: (_, r) => <StatusPill ok={r.status === 1} />,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 150,
      hideInSearch: true,
      render: (_, r) => <span className="mono">{fmtTime(r.created_at)}</span>,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 240,
      render: (_, r) => (
        <Space size={4} wrap>
          {hasPerm('system:user:update') && (
            <a key="edit" onClick={() => setEditUser(r)}>
              编辑
            </a>
          )}
          {hasPerm('system:user:assign-role') && (
            <a
              key="roles"
              onClick={async () => {
                try {
                  const u = await getUser(r.id);
                  setRoleIds(u.role_ids ?? []);
                } catch {
                  setRoleIds([]);
                }
                setRoleUser(r);
              }}
            >
              分配角色
            </a>
          )}
          {hasPerm('system:user:reset-pwd') && (
            <a key="pwd" onClick={() => setPwdUser(r)}>
              重置密码
            </a>
          )}
          {hasPerm('system:user:delete') && (
            <Popconfirm
              key="del"
              title={`确认删除用户「${r.username}」？`}
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={async () => {
                await deleteUser(r.id);
                message.success('已删除');
                actionRef.current?.reload();
              }}
            >
              <a style={{ color: '#FF4D4F' }}>删除</a>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div className="gk-page-head fade-up">
        <div className="gk-page-kicker">System · Users</div>
        <h2 className="gk-page-title">用户管理</h2>
      </div>

      <ProTable<User>
        className="fade-up"
        style={{ '--d': '120ms' } as CSSProperties}
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        options={false}
        pagination={{ defaultPageSize: 20, showSizeChanger: true }}
        request={async (params) => {
          const q = params as { current?: number; pageSize?: number; keyword?: string; status?: number };
          const data = await listUsers({
            page: q.current,
            page_size: q.pageSize,
            keyword: q.keyword,
            status: q.status,
          });
          return { data: data?.list ?? [], total: data?.total ?? 0, success: true };
        }}
        toolBarRender={() => [
          hasPerm('system:user:create') && (
            <Button key="create" type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
              新建用户
            </Button>
          ),
        ]}
      />

      {/* 新建 */}
      <ModalForm
        title="新建用户"
        open={createOpen}
        modalProps={{ destroyOnClose: true, onCancel: () => setCreateOpen(false), maskClosable: false }}
        onFinish={async (values) => {
          await createUser({
            username: values.username,
            password: values.password,
            nickname: values.nickname,
            email: values.email,
            dept_id: values.dept_id,
            role_ids: values.role_ids,
          });
          message.success('创建成功');
          setCreateOpen(false);
          actionRef.current?.reload();
          return true;
        }}
      >
        <ProFormText
          name="username"
          label="用户名"
          rules={[
            { required: true, message: '请输入用户名' },
            { min: 3, max: 64, message: '长度 3-64 位' },
          ]}
        />
        <ProFormText.Password
          name="password"
          label="初始密码"
          rules={[
            { required: true, message: '请输入初始密码' },
            { min: 8, message: '密码最少 8 位' },
          ]}
        />
        <ProFormText name="nickname" label="昵称" />
        <ProFormText name="email" label="邮箱" rules={[{ type: 'email', message: '邮箱格式不正确' }]} />
        <ProFormTreeSelect
          name="dept_id"
          label="部门"
          fieldProps={{
            treeData: deptOptions,
            allowClear: true,
            treeDefaultExpandAll: true,
            placeholder: '选择部门',
          }}
        />
        <ProFormSelect
          name="role_ids"
          label="角色"
          fieldProps={{ mode: 'multiple', options: roleOptions, allowClear: true, placeholder: '选择角色' }}
        />
      </ModalForm>

      {/* 编辑 */}
      <ModalForm
        title={`编辑用户「${editUser?.username ?? ''}」`}
        open={!!editUser}
        modalProps={{ destroyOnClose: true, onCancel: () => setEditUser(null), maskClosable: false }}
        initialValues={
          editUser
            ? {
                nickname: editUser.nickname,
                email: editUser.email,
                dept_id: editUser.dept_id || undefined,
                status: editUser.status,
              }
            : {}
        }
        onFinish={async (values) => {
          if (!editUser) return false;
          await updateUser(editUser.id, {
            nickname: values.nickname,
            email: values.email,
            dept_id: values.dept_id,
            status: values.status,
          });
          message.success('已更新');
          setEditUser(null);
          actionRef.current?.reload();
          return true;
        }}
      >
        <ProFormText name="nickname" label="昵称" />
        <ProFormText name="email" label="邮箱" rules={[{ type: 'email', message: '邮箱格式不正确' }]} />
        <ProFormTreeSelect
          name="dept_id"
          label="部门"
          fieldProps={{ treeData: deptOptions, allowClear: true, treeDefaultExpandAll: true }}
        />
        <ProFormRadio.Group
          name="status"
          label="状态"
          rules={[{ required: true }]}
          options={[
            { label: '启用', value: 1 },
            { label: '禁用', value: 0 },
          ]}
        />
      </ModalForm>

      {/* 分配角色 */}
      <Modal
        title={`给「${roleUser?.username ?? ''}」分配角色`}
        open={!!roleUser}
        onCancel={() => setRoleUser(null)}
        okText="保存"
        cancelText="取消"
        destroyOnClose
        onOk={async () => {
          if (!roleUser) return;
          await assignUserRoles(roleUser.id, roleIds);
          message.success('已分配');
          setRoleUser(null);
          actionRef.current?.reload();
        }}
      >
        <Select
          mode="multiple"
          style={{ width: '100%', marginTop: 8 }}
          placeholder="选择角色"
          options={roleOptions}
          value={roleIds}
          onChange={setRoleIds}
          allowClear
        />
      </Modal>

      {/* 重置密码 */}
      <ModalForm
        title={`重置「${pwdUser?.username ?? ''}」的密码`}
        open={!!pwdUser}
        modalProps={{ destroyOnClose: true, onCancel: () => setPwdUser(null), maskClosable: false }}
        onFinish={async (values) => {
          if (!pwdUser) return false;
          await resetUserPassword(pwdUser.id, values.password);
          message.success('密码已重置');
          setPwdUser(null);
          return true;
        }}
      >
        <ProFormText.Password
          name="password"
          label="新密码"
          rules={[
            { required: true, message: '请输入新密码' },
            { min: 8, message: '密码最少 8 位' },
          ]}
        />
      </ModalForm>
    </div>
  );
}
