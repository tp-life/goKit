import { useCallback, useEffect, useMemo, useState } from 'react';
import type { CSSProperties } from 'react';
import { App, Button, Card, Input, Popconfirm, Space, Table } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ModalForm,
  ProFormDigit,
  ProFormRadio,
  ProFormText,
  ProFormTreeSelect,
} from '@ant-design/pro-components';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { createDept, deleteDept, getDeptTree, updateDept } from '../../api/dept';
import type { DeptNode } from '../../api/types';
import { deptTreeToSelect, filterTree } from '../../utils/tree';
import { usePerms } from '../../store/auth';
import StatusPill from '../../components/StatusPill';

export default function DeptsPage() {
  const { message } = App.useApp();
  const { hasPerm } = usePerms();
  const [tree, setTree] = useState<DeptNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [editDept, setEditDept] = useState<DeptNode | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await getDeptTree();
      setTree(data ?? []);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const kw = keyword.trim();
  const filteredTree = useMemo(
    () => (kw ? filterTree(tree, (n) => n.name.includes(kw)) : tree),
    [tree, kw],
  );

  const columns: ColumnsType<DeptNode> = [
    { title: '部门名称', dataIndex: 'name' },
    { title: 'ID', dataIndex: 'id', width: 90, render: (v) => <span className="mono">{v}</span> },
    { title: '排序', dataIndex: 'sort', width: 80, render: (v) => <span className="mono">{v ?? 0}</span> },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (v) => <StatusPill ok={v === 1} />,
    },
    {
      title: '操作',
      key: 'op',
      width: 140,
      render: (_, r) => (
        <Space size={4}>
          {hasPerm('system:dept:update') && (
            <a onClick={() => setEditDept(r)}>编辑</a>
          )}
          {hasPerm('system:dept:delete') && (
            <Popconfirm
              title={`确认删除部门「${r.name}」？`}
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={async () => {
                await deleteDept(r.id);
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

  return (
    <div>
      <div className="gk-page-head fade-up">
        <div className="gk-page-kicker">System · Departments</div>
        <h2 className="gk-page-title">部门管理</h2>
      </div>

      <Card
        className="fade-up"
        style={{ '--d': '120ms' } as CSSProperties}
        title="部门树"
        extra={
          <Space>
            <Input.Search
              allowClear
              placeholder="搜索部门名称"
              style={{ width: 200 }}
              onChange={(e) => setKeyword(e.target.value)}
            />
            <Button icon={<ReloadOutlined />} onClick={load}>
              刷新
            </Button>
            {hasPerm('system:dept:create') && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
                新建部门
              </Button>
            )}
          </Space>
        }
      >
        <Table<DeptNode>
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
        title="新建部门"
        open={createOpen}
        modalProps={{ destroyOnClose: true, onCancel: () => setCreateOpen(false), maskClosable: false }}
        onFinish={async (values) => {
          await createDept({
            parent_id: values.parent_id ?? 0,
            name: values.name,
            sort: values.sort ?? 0,
          });
          message.success('创建成功');
          setCreateOpen(false);
          load();
          return true;
        }}
      >
        <ProFormTreeSelect
          name="parent_id"
          label="父部门"
          tooltip="不选则为根部门"
          fieldProps={{
            treeData: deptTreeToSelect(tree),
            allowClear: true,
            treeDefaultExpandAll: true,
            placeholder: '不选则为根部门',
          }}
        />
        <ProFormText name="name" label="部门名称" rules={[{ required: true, message: '请输入部门名称' }]} />
        <ProFormDigit name="sort" label="排序" min={0} fieldProps={{ precision: 0 }} initialValue={0} />
      </ModalForm>

      {/* 编辑 */}
      <ModalForm
        title={`编辑部门「${editDept?.name ?? ''}」`}
        open={!!editDept}
        modalProps={{ destroyOnClose: true, onCancel: () => setEditDept(null), maskClosable: false }}
        initialValues={
          editDept
            ? {
                parent_id: editDept.parent_id || undefined,
                name: editDept.name,
                sort: editDept.sort,
                status: editDept.status,
              }
            : {}
        }
        onFinish={async (values) => {
          if (!editDept) return false;
          await updateDept(editDept.id, {
            parent_id: values.parent_id ?? 0,
            name: values.name,
            sort: values.sort ?? 0,
            status: values.status,
          });
          message.success('已更新');
          setEditDept(null);
          load();
          return true;
        }}
      >
        <ProFormTreeSelect
          name="parent_id"
          label="父部门"
          tooltip="不选则为根部门"
          fieldProps={{
            treeData: deptTreeToSelect(tree),
            allowClear: true,
            treeDefaultExpandAll: true,
            placeholder: '不选则为根部门',
          }}
        />
        <ProFormText name="name" label="部门名称" rules={[{ required: true, message: '请输入部门名称' }]} />
        <ProFormDigit name="sort" label="排序" min={0} fieldProps={{ precision: 0 }} />
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
