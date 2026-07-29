import { http } from './request';
import type { CreateRoleReq, PageData, PageQuery, Role, UpdateRoleReq } from './types';

export function listRoles(params: PageQuery) {
  return http.get<PageData<Role>>('/roles', params);
}

export function createRole(req: CreateRoleReq) {
  return http.post<{ id: number }>('/roles', req);
}

export function getRole(id: number) {
  return http.get<Role>(`/roles/${id}`);
}

export function updateRole(id: number, req: UpdateRoleReq) {
  return http.put<void>(`/roles/${id}`, req);
}

export function deleteRole(id: number) {
  return http.delete<void>(`/roles/${id}`);
}

export function assignRoleMenus(id: number, menu_ids: number[]) {
  return http.put<void>(`/roles/${id}/menus`, { menu_ids });
}

export function getRoleUsers(id: number) {
  return http.get<{ user_ids: number[] }>(`/roles/${id}/users`);
}

export function assignRoleUsers(id: number, user_ids: number[]) {
  return http.put<void>(`/roles/${id}/users`, { user_ids });
}
