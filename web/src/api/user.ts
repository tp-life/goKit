import { http } from './request';
import type { CreateUserReq, PageData, PageQuery, UpdateUserReq, User } from './types';

export function listUsers(params: PageQuery) {
  return http.get<PageData<User>>('/users', params);
}

export function createUser(req: CreateUserReq) {
  return http.post<{ id: number }>('/users', req);
}

export function getUser(id: number) {
  return http.get<User>(`/users/${id}`);
}

export function updateUser(id: number, req: UpdateUserReq) {
  return http.put<void>(`/users/${id}`, req);
}

export function deleteUser(id: number) {
  return http.delete<void>(`/users/${id}`);
}

export function assignUserRoles(id: number, role_ids: number[]) {
  return http.put<void>(`/users/${id}/roles`, { role_ids });
}

export function resetUserPassword(id: number, password: string) {
  return http.put<void>(`/users/${id}/password`, { password });
}
