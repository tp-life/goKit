import { useMemo, useState } from 'react';
import { Button, Checkbox, Empty, Input } from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import type { MenuNode } from '../api/types';
import { menuTreeToPermGroups } from '../utils/tree';
import type { PermGroup } from '../utils/tree';

interface PermPickerProps {
  /** 完整菜单树 */
  tree: MenuNode[];
  /** 已选中的叶子节点 id */
  value: number[];
  onChange: (ids: number[]) => void;
}

/** 平铺式权限选择器：按菜单分组平铺叶子权限，支持搜索、分组全选 */
export default function PermPicker({ tree, value, onChange }: PermPickerProps) {
  const [keyword, setKeyword] = useState('');

  const groups = useMemo(() => menuTreeToPermGroups(tree), [tree]);
  const selected = useMemo(() => new Set(value), [value]);
  const totalLeaves = useMemo(
    () => groups.reduce((sum, g) => sum + g.leaves.length, 0),
    [groups],
  );

  const kw = keyword.trim().toLowerCase();
  const visibleGroups = useMemo(() => {
    if (!kw) return groups;
    return groups
      .map((g) => ({
        ...g,
        leaves: g.leaves.filter(
          (l) =>
            l.title.toLowerCase().includes(kw) ||
            (l.perm_code ?? '').toLowerCase().includes(kw),
        ),
      }))
      .filter((g) => g.leaves.length > 0);
  }, [groups, kw]);

  const toggle = (id: number) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange([...next]);
  };

  const toggleGroup = (g: PermGroup, checked: boolean) => {
    const next = new Set(selected);
    for (const l of g.leaves) {
      if (checked) next.add(l.id);
      else next.delete(l.id);
    }
    onChange([...next]);
  };

  return (
    <div className="perm-picker">
      <div className="perm-toolbar">
        <Input
          allowClear
          prefix={<SearchOutlined style={{ color: 'rgba(136, 204, 153, 0.55)' }} />}
          placeholder="搜索权限名称 / 权限点"
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
        />
        <Button type="text" size="small" onClick={() => onChange(groups.flatMap((g) => g.leaves.map((l) => l.id)))}>
          全选
        </Button>
        <Button type="text" size="small" onClick={() => onChange([])}>
          清空
        </Button>
        <span className="perm-count mono">
          已选 {value.length} / {totalLeaves}
        </span>
      </div>

      <div className="perm-list">
        {visibleGroups.length === 0 && (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的权限" style={{ padding: '32px 0' }} />
        )}
        {visibleGroups.map((g) => {
          const selCount = g.leaves.filter((l) => selected.has(l.id)).length;
          const allChecked = selCount === g.leaves.length && g.leaves.length > 0;
          const title = g.path.length ? g.path[g.path.length - 1] : g.leaves[0]?.title;
          const crumbs = g.path.slice(0, -1);
          return (
            <div className="perm-group" key={g.key}>
              <div className="perm-group-head">
                <Checkbox
                  checked={allChecked}
                  indeterminate={selCount > 0 && !allChecked}
                  onChange={(e) => toggleGroup(g, e.target.checked)}
                >
                  {crumbs.length > 0 && <span className="perm-group-path">{crumbs.join(' / ')}</span>}
                  <span className="perm-group-title">{title}</span>
                </Checkbox>
                <span className="perm-group-count mono">
                  {selCount}/{g.leaves.length}
                </span>
              </div>
              <div className="perm-grid">
                {g.leaves.map((l) => {
                  const on = selected.has(l.id);
                  return (
                    <button
                      type="button"
                      key={l.id}
                      className={`perm-chip${on ? ' is-on' : ''}`}
                      onClick={() => toggle(l.id)}
                    >
                      <span className="perm-chip-title">{l.title}</span>
                      {l.perm_code ? <span className="perm-chip-code">{l.perm_code}</span> : null}
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
