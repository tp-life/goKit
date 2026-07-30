import type { DeptNode, MenuNode } from '../api/types';

export interface TreeSelectNode {
  value: number;
  title: string;
  children?: TreeSelectNode[];
}

export interface PermLeaf {
  id: number;
  title: string;
  perm_code: string;
  type: number; // 1 目录 / 2 菜单 / 3 按钮
}

export interface PermGroup {
  key: string;
  path: string[]; // 分组标题路径，如 ['系统管理', '用户管理']
  leaves: PermLeaf[];
}

/** 部门树 → TreeSelect treeData */
export function deptTreeToSelect(nodes: DeptNode[]): TreeSelectNode[] {
  return (nodes ?? []).map((n) => ({
    value: n.id,
    title: n.name,
    children: n.children?.length ? deptTreeToSelect(n.children) : undefined,
  }));
}

/** 部门树 → id/name 映射 */
export function flattenDepts(nodes: DeptNode[], map: Record<number, string> = {}) {
  for (const n of nodes ?? []) {
    map[n.id] = n.name;
    if (n.children?.length) flattenDepts(n.children, map);
  }
  return map;
}

/** 菜单树 → TreeSelect treeData */
export function menuTreeToSelect(nodes: MenuNode[]): TreeSelectNode[] {
  return (nodes ?? []).map((n) => ({
    value: n.id,
    title: n.title,
    children: n.children?.length ? menuTreeToSelect(n.children) : undefined,
  }));
}

/**
 * 菜单树 → 平铺权限分组。
 * 「子节点全部为叶子」的节点成为一个分组（标题带上层路径）；
 * 根级叶子或混合层级下的散叶子各自成为单条分组。
 */
export function menuTreeToPermGroups(nodes: MenuNode[]): PermGroup[] {
  const groups: PermGroup[] = [];
  const toLeaf = (n: MenuNode): PermLeaf => ({
    id: n.id,
    title: n.title,
    perm_code: n.perm_code,
    type: n.type,
  });
  const walk = (node: MenuNode, path: string[]) => {
    const children = node.children ?? [];
    if (!children.length) {
      groups.push({ key: `leaf-${node.id}`, path, leaves: [toLeaf(node)] });
      return;
    }
    if (children.every((c) => !c.children?.length)) {
      groups.push({ key: `grp-${node.id}`, path: [...path, node.title], leaves: children.map(toLeaf) });
      return;
    }
    for (const c of children) walk(c, [...path, node.title]);
  };
  for (const n of nodes ?? []) walk(n, []);
  return groups;
}

/**
 * 由选中的叶子 id 集合推导完整提交列表：
 * 叶子原样保留，所有后代叶子均被选中的祖先节点一并勾选（与 antd Tree checkable 行为一致）。
 */
export function withFullAncestors(nodes: MenuNode[], selected: Set<number>): number[] {
  const out: number[] = [];
  const walk = (node: MenuNode): boolean => {
    const children = node.children ?? [];
    if (!children.length) {
      if (selected.has(node.id)) {
        out.push(node.id);
        return true;
      }
      return false;
    }
    const allSelected = children.map(walk).every(Boolean);
    if (allSelected) out.push(node.id);
    return allSelected;
  };
  for (const n of nodes ?? []) walk(n);
  return out;
}

/** 收集菜单树的全部叶子节点 id */
export function collectMenuLeafIds(nodes: MenuNode[], acc: Set<number> = new Set()) {
  for (const n of nodes ?? []) {
    if (n.children?.length) collectMenuLeafIds(n.children, acc);
    else acc.add(n.id);
  }
  return acc;
}

/** 按条件过滤树：命中节点及其祖先保留（祖先仅作路径展示） */
export function filterTree<T extends { children?: T[] }>(
  nodes: T[],
  match: (n: T) => boolean,
): T[] {
  const walk = (list: T[]): T[] => {
    const out: T[] = [];
    for (const n of list ?? []) {
      const children = n.children?.length ? walk(n.children) : undefined;
      if (match(n) || (children && children.length > 0)) {
        out.push({ ...n, children });
      }
    }
    return out;
  };
  return walk(nodes ?? []);
}
