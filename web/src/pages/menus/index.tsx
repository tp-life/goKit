import { useCallback, useEffect, useMemo, useState } from 'react';
import type { CSSProperties } from 'react';
import { App, Button, Card, Input, Popconfirm, Space, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ModalForm,
  ProFormDependency,
  ProFormDigit,
  ProFormRadio,
  ProFormText,
  ProFormTreeSelect,
} from '@ant-design/pro-components';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { createMenu, deleteMenu, getMenuTree, updateMenu } from '../../api/menu';
import type { MenuNode } from '../../api/types';
import { filterTree, menuTreeToSelect } from '../../utils/tree';
import { MENU_TYPE_TEXT } from '../../utils/format';
import { usePerms } from '../../store/auth';
import StatusPill from '../../components/StatusPill';

const TYPE_COLORS: Record<number, string> = {
  1: 'var(--text-secondary)',
  2: 'var(--accent-cyan)',
  3: 'var(--accent-purple)',
};

export default function MenusPage() {
  const { message } = App.useApp();
  const { hasPerm } = usePerms();
  const [tree, setTree] = useState<MenuNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [editMenu, setEditMenu] = useState<MenuNode | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await getMenuTree();
      setTree(data ?? []);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const kw = keyword.trim().toLowerCase();
  const filteredTree = useMemo(
    () =>
      kw
        ? filterTree(tree, (n) =>
            [n.title, n.path, n.perm_code].some((f) => f?.toLowerCase().includes(kw)),
          )
        : tree,
    [tree, kw],
  );

  const columns: ColumnsType<MenuNode> = [
    { title: '标题', dataIndex: 'title' },
    {
      title: '类型',
      dataIndex: 'type',
      width: 90,
      render: (v) => (
        <Tag color={TYPE_COLORS[v]} style={{ border: '1px solid currentColor', background: 'transparent', color: TYPE_COLORS[v] }}>
          {MENU_TYPE_TEXT[v] ?? v}
        </Tag>
      ),
    },
    { title: '路径', dataIndex: 'path', render: (v) => (v ? <span className="mono">{v}</span> : '—') },
    {
      title: '权限点',
      dataIndex: 'perm_code',
      render: (v) => (v ? <span className="mono">{v}</span> : '—'),
    },
    { title: '排序', dataIndex: 'sort', width: 80, render: (v) => <span className="mono">{v ?? 0}</span> },
    { title: '状态', dataIndex: 'status', width: 100, render: (v) => <StatusPill ok={v === 1} /> },
    {
      title: '操作',
      key: 'op',
      width: 140,
      render: (_, r) => (
        <Space size={4}>
          {hasPerm('system:menu:update') && <a onClick={() => setEditMenu(r)}>编辑</a>}
          {hasPerm('system:menu:delete') && (
            <Popconfirm
              title={`确认删除「${r.title}」？`}
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={async () => {
                await deleteMenu(r.id);
                message.success('已删除');
                load();
              }}
            >
              <a style={{ color: '#FF4D4F' }}>删除</a>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  const formItems = (
    <>
      <ProFormTreeSelect
        name="parent_id"
        label="父节点"
        tooltip="不选则为根节点"
        fieldProps={{
          treeData: menuTreeToSelect(tree),
          allowClear: true,
          treeDefaultExpandAll: true,
          placeholder: '不选则为根节点',
        }}
      />
      <ProFormText name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]} />
      <ProFormRadio.Group
        name="type"
        label="类型"
        initialValue={2}
        rules={[{ required: true }]}
        options={[
          { label: '目录', value: 1 },
          { label: '菜单', value: 2 },
          { label: '按钮', value: 3 },
        ]}
      />
      <ProFormDependency name={['type']}>
        {({ type }) => (
          <>
            {type !== 3 && <ProFormText name="path" label="路由路径" placeholder="/system/users" />}
            {type === 3 && (
              <ProFormText
                name="perm_code"
                label="权限点"
                placeholder="system:user:list"
                rules={[{ required: true, message: '按钮节点需要权限点' }]}
              />
            )}
          </>
        )}
      </ProFormDependency>
      <ProFormDigit name="sort" label="排序" min={0} fieldProps={{ precision: 0 }} initialValue={0} />
    </>
  );

  return (
    <div>
      <div className="gk-page-head fade-up">
        <div className="gk-page-kicker">System · Menus</div>
        <h2 className="gk-page-title">菜单管理</h2>
      </div>

      <Card
        className="fade-up"
        style={{ '--d': '120ms' } as CSSProperties}
        title="菜单树"
        extra={
          <Space>
            <Input.Search
              allowClear
              placeholder="搜索标题 / 路径 / 权限点"
              style={{ width: 220 }}
              onChange={(e) => setKeyword(e.target.value)}
            />
            <Button icon={<ReloadOutlined />} onClick={load}>
              刷新
            </Button>
            {hasPerm('system:menu:create') && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
                新建菜单
              </Button>
            )}
          </Space>
        }
      >
        <Table<MenuNode>
          rowKey="id"
          columns={columns}
          dataSource={filteredTree}
          loading={loading}
          pagination={false}
          expandable={{ defaultExpandAllRows: true }}
        />
      </Card>

      {/* 新建 */}
      <ModalForm
        title="新建菜单"
        open={createOpen}
        modalProps={{ destroyOnClose: true, onCancel: () => setCreateOpen(false), maskClosable: false }}
        onFinish={async (values) => {
          await createMenu({
            parent_id: values.parent_id ?? 0,
            title: values.title,
            type: values.type,
            path: values.type === 3 ? undefined : values.path,
            perm_code: values.type === 3 ? values.perm_code : undefined,
            sort: values.sort ?? 0,
          });
          message.success('创建成功');
          setCreateOpen(false);
          load();
          return true;
        }}
      >
        {formItems}
      </ModalForm>

      {/* 编辑 */}
      <ModalForm
        title={`编辑「${editMenu?.title ?? ''}」`}
        open={!!editMenu}
        modalProps={{ destroyOnClose: true, onCancel: () => setEditMenu(null), maskClosable: false }}
        initialValues={
          editMenu
            ? {
                parent_id: editMenu.parent_id || undefined,
                title: editMenu.title,
                type: editMenu.type,
                path: editMenu.path,
                perm_code: editMenu.perm_code,
                sort: editMenu.sort,
                status: editMenu.status,
              }
            : {}
        }
        onFinish={async (values) => {
          if (!editMenu) return false;
          await updateMenu(editMenu.id, {
            parent_id: values.parent_id ?? 0,
            title: values.title,
            type: values.type,
            path: values.type === 3 ? '' : values.path ?? '',
            perm_code: values.type === 3 ? values.perm_code : '',
            sort: values.sort ?? 0,
            status: values.status,
          });
          message.success('已更新');
          setEditMenu(null);
          load();
          return true;
        }}
      >
        {formItems}
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
    </div>
  );
}
