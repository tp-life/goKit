import { useEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import { App, Button, Modal, Popconfirm, Space, Transfer } from 'antd';
import {
  ModalForm,
  ProFormDependency,
  ProFormRadio,
  ProFormSelect,
  ProFormText,
  ProFormTextArea,
  ProFormTreeSelect,
  ProTable,
} from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PlusOutlined } from '@ant-design/icons';
import {
  assignRoleMenus,
  assignRoleUsers,
  createRole,
  deleteRole,
  getRole,
  getRoleUsers,
  listRoles,
  updateRole,
} from '../../api/role';
import { getMenuTree } from '../../api/menu';
import { getDeptTree } from '../../api/dept';
import { listUsers } from '../../api/user';
import type { MenuNode, Role } from '../../api/types';
import { collectMenuLeafIds, deptTreeToSelect, withFullAncestors } from '../../utils/tree';
import type { TreeSelectNode } from '../../utils/tree';
import { DATA_SCOPE_TEXT, fmtTime } from '../../utils/format';
import { usePerms } from '../../store/auth';
import StatusPill from '../../components/StatusPill';
import PermPicker from '../../components/PermPicker';

const dataScopeOptions = Object.entries(DATA_SCOPE_TEXT).map(([v, label]) => ({
  value: Number(v),
  label,
}));

export default function RolesPage() {
  const { message } = App.useApp();
  const { hasPerm } = usePerms();
  const actionRef = useRef<ActionType>();

  const [deptOptions, setDeptOptions] = useState<TreeSelectNode[]>([]);

  const [createOpen, setCreateOpen] = useState(false);
  const [editRole, setEditRole] = useState<Role | null>(null);

  const [menuRole, setMenuRole] = useState<Role | null>(null);
  const [menuTree, setMenuTree] = useState<MenuNode[]>([]);
  const [checkedMenuIds, setCheckedMenuIds] = useState<number[]>([]);

  const [userRole, setUserRole] = useState<Role | null>(null);
  const [allUsers, setAllUsers] = useState<{ key: string; title: string }[]>([]);
  const [targetUserKeys, setTargetUserKeys] = useState<string[]>([]);

  useEffect(() => {
    getDeptTree()
      .then((tree) => setDeptOptions(deptTreeToSelect(tree ?? [])))
      .catch(() => {});
  }, []);

  const openMenuModal = async (r: Role) => {
    const [tree, detail] = await Promise.all([getMenuTree(), getRole(r.id)]);
    const leafIds = collectMenuLeafIds(tree ?? []);
    setMenuTree(tree ?? []);
    // 平铺选择器只操作叶子节点；提交时再推导全选祖先一并回传
    setCheckedMenuIds((detail.menu_ids ?? []).filter((id) => leafIds.has(id)));
    setMenuRole(r);
  };

  const openUserModal = async (r: Role) => {
    const [users, ids] = await Promise.all([
      listUsers({ page: 1, page_size: 100 }),
      getRoleUsers(r.id),
    ]);
    setAllUsers(
      (users?.list ?? []).map((u) => ({
        key: String(u.id),
        title: `${u.nickname || u.username}（@${u.username}）`,
      })),
    );
    setTargetUserKeys((ids?.user_ids ?? []).map(String));
    setUserRole(r);
  };

  const columns: ProColumns<Role>[] = [
    {
      title: '关键字',
      dataIndex: 'keyword',
      hideInTable: true,
      fieldProps: { placeholder: '角色名称 / 编码' },
    },
    { title: 'ID', dataIndex: 'id', width: 72, hideInSearch: true, render: (_, r) => <span className="mono">{r.id}</span> },
    { title: '角色名称', dataIndex: 'name', hideInSearch: true },
    { title: '编码', dataIndex: 'code', hideInSearch: true, render: (_, r) => <span className="mono">{r.code}</span> },
    {
      title: '数据范围',
      dataIndex: 'data_scope',
      width: 130,
      hideInSearch: true,
      render: (_, r) => DATA_SCOPE_TEXT[r.data_scope] ?? r.data_scope,
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
    { title: '备注', dataIndex: 'remark', ellipsis: true, hideInSearch: true, render: (v) => v || '—' },
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
      width: 260,
      render: (_, r) => (
        <Space size={4} wrap>
          {hasPerm('system:role:update') && (
            <a key="edit" onClick={() => setEditRole(r)}>
              编辑
            </a>
          )}
          {hasPerm('system:role:assign-menu') && (
            <a key="menus" onClick={() => openMenuModal(r)}>
              菜单权限
            </a>
          )}
          {hasPerm('system:role:assign-user') && (
            <a key="users" onClick={() => openUserModal(r)}>
              分配用户
            </a>
          )}
          {hasPerm('system:role:delete') && (
            <Popconfirm
              key="del"
              title={`确认删除角色「${r.name}」？`}
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={async () => {
                await deleteRole(r.id);
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

  const roleFormItems = (
    <>
      <ProFormText name="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]} />
      <ProFormText name="code" label="角色编码" rules={[{ required: true, message: '请输入角色编码' }]} />
      <ProFormSelect
        name="data_scope"
        label="数据范围"
        options={dataScopeOptions}
        initialValue={1}
        rules={[{ required: true }]}
      />
      <ProFormDependency name={['data_scope']}>
        {({ data_scope }) =>
          data_scope === 2 ? (
            <ProFormTreeSelect
              name="dept_ids"
              label="自定义部门"
              fieldProps={{
                treeData: deptOptions,
                multiple: true,
                allowClear: true,
                treeDefaultExpandAll: true,
                placeholder: '选择部门（可多选）',
              }}
            />
          ) : null
        }
      </ProFormDependency>
      <ProFormTextArea name="remark" label="备注" />
    </>
  );

  return (
    <div>
      <div className="gk-page-head fade-up">
        <div className="gk-page-kicker">System · Roles</div>
        <h2 className="gk-page-title">角色管理</h2>
      </div>

      <ProTable<Role>
        className="fade-up"
        style={{ '--d': '120ms' } as CSSProperties}
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        options={false}
        pagination={{ defaultPageSize: 20, showSizeChanger: true }}
        request={async (params) => {
          const q = params as { current?: number; pageSize?: number; keyword?: string; status?: number };
          const data = await listRoles({
            page: q.current,
            page_size: q.pageSize,
            keyword: q.keyword,
            status: q.status,
          });
          return { data: data?.list ?? [], total: data?.total ?? 0, success: true };
        }}
        toolBarRender={() => [
          hasPerm('system:role:create') && (
            <Button key="create" type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
              新建角色
            </Button>
          ),
        ]}
      />

      {/* 新建 */}
      <ModalForm
        title="新建角色"
        open={createOpen}
        modalProps={{ destroyOnClose: true, onCancel: () => setCreateOpen(false), maskClosable: false }}
        onFinish={async (values) => {
          await createRole({
            name: values.name,
            code: values.code,
            data_scope: values.data_scope,
            remark: values.remark,
            dept_ids: values.data_scope === 2 ? values.dept_ids : undefined,
          });
          message.success('创建成功');
          setCreateOpen(false);
          actionRef.current?.reload();
          return true;
        }}
      >
        {roleFormItems}
      </ModalForm>

      {/* 编辑 */}
      <ModalForm
        title={`编辑角色「${editRole?.name ?? ''}」`}
        open={!!editRole}
        modalProps={{ destroyOnClose: true, onCancel: () => setEditRole(null), maskClosable: false }}
        initialValues={
          editRole
            ? {
                name: editRole.name,
                code: editRole.code,
                data_scope: editRole.data_scope,
                status: editRole.status,
                remark: editRole.remark,
                dept_ids: editRole.dept_ids,
              }
            : {}
        }
        onFinish={async (values) => {
          if (!editRole) return false;
          await updateRole(editRole.id, {
            name: values.name,
            code: values.code,
            data_scope: values.data_scope,
            status: values.status,
            remark: values.remark,
            dept_ids: values.data_scope === 2 ? values.dept_ids : [],
          });
          message.success('已更新');
          setEditRole(null);
          actionRef.current?.reload();
          return true;
        }}
      >
        {roleFormItems}
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

      {/* 菜单权限 */}
      <Modal
        title={`「${menuRole?.name ?? ''}」的菜单权限`}
        open={!!menuRole}
        onCancel={() => setMenuRole(null)}
        okText="保存"
        cancelText="取消"
        width={760}
        destroyOnClose
        onOk={async () => {
          if (!menuRole) return;
          await assignRoleMenus(menuRole.id, withFullAncestors(menuTree, new Set(checkedMenuIds)));
          message.success('已保存');
          setMenuRole(null);
        }}
      >
        <PermPicker tree={menuTree} value={checkedMenuIds} onChange={setCheckedMenuIds} />
      </Modal>

      {/* 分配用户 */}
      <Modal
        title={`给「${userRole?.name ?? ''}」分配用户`}
        open={!!userRole}
        onCancel={() => setUserRole(null)}
        okText="保存"
        cancelText="取消"
        width={640}
        destroyOnClose
        onOk={async () => {
          if (!userRole) return;
          await assignRoleUsers(userRole.id, targetUserKeys.map(Number));
          message.success('已保存');
          setUserRole(null);
        }}
      >
        <Transfer
          style={{ marginTop: 8 }}
          dataSource={allUsers}
          targetKeys={targetUserKeys}
          onChange={(keys) => setTargetUserKeys(keys as string[])}
          render={(item) => item.title}
          titles={['候选用户', '已分配']}
          showSearch
          listStyle={{ width: 280, height: 320 }}
        />
      </Modal>
    </div>
  );
}
