// 与 api/openapi.yaml 对应的类型定义（snake_case 字段名以后端为准）

export interface BaseResponse<T = unknown> {
  code: number;
  message: string;
  data?: T;
}

// ---------- 认证 ----------
export interface LoginReq {
  username: string;
  password: string;
}

export interface LoginData {
  token: string;
}

export interface Profile {
  id: number;
  username: string;
  nickname: string;
  email: string;
  dept_id: number;
  is_super: boolean;
  roles?: string[];
  perms?: string[];
}

export interface ChangePwdReq {
  old_password: string;
  new_password: string;
}

// ---------- 用户 ----------
export interface User {
  id: number;
  username: string;
  nickname: string;
  email: string;
  dept_id: number;
  status: number; // 0 禁用 / 1 启用
  is_super: boolean;
  role_ids?: number[];
  created_at: string;
}

export interface PageData<T> {
  list: T[];
  total: number;
}

export interface CreateUserReq {
  username: string;
  password: string;
  nickname?: string;
  email?: string;
  dept_id?: number;
  role_ids?: number[];
}

export interface UpdateUserReq {
  nickname?: string;
  email?: string;
  dept_id?: number;
  status?: number;
}

// ---------- 角色 ----------
export interface Role {
  id: number;
  name: string;
  code: string;
  data_scope: number; // 1 全部 / 2 自定义部门 / 3 本部门及以下 / 4 本部门 / 5 仅本人
  status: number;
  remark: string;
  menu_ids?: number[];
  dept_ids?: number[];
  created_at: string;
}

export interface CreateRoleReq {
  name: string;
  code: string;
  data_scope?: number;
  remark?: string;
  menu_ids?: number[];
  dept_ids?: number[];
}

export interface UpdateRoleReq {
  name: string;
  code: string;
  data_scope?: number;
  status?: number;
  remark?: string;
  dept_ids?: number[];
}

// ---------- 部门 ----------
export interface DeptNode {
  id: number;
  parent_id: number;
  name: string;
  sort: number;
  status: number;
  children?: DeptNode[];
}

export interface CreateDeptReq {
  parent_id?: number;
  name: string;
  sort?: number;
}

export interface UpdateDeptReq {
  parent_id?: number;
  name: string;
  sort?: number;
  status?: number;
}

// ---------- 菜单 ----------
export interface MenuNode {
  id: number;
  parent_id: number;
  title: string;
  type: number; // 1 目录 / 2 菜单 / 3 按钮
  path: string;
  perm_code: string;
  sort: number;
  status: number;
  children?: MenuNode[];
}

export interface CreateMenuReq {
  parent_id?: number;
  title: string;
  type?: number;
  path?: string;
  perm_code?: string;
  sort?: number;
}

export interface UpdateMenuReq extends CreateMenuReq {
  status?: number;
}

// ---------- 日志 ----------
export interface OpLog {
  id: number;
  user_id: number;
  username: string;
  method: string;
  path: string;
  ip: string;
  status: number;
  latency_ms: number;
  created_at: string;
}

export interface PageQuery {
  page?: number;
  page_size?: number;
  [key: string]: string | number | boolean | undefined;
}
