import { http } from './request';
import type { CreateDeptReq, DeptNode, UpdateDeptReq } from './types';

export function getDeptTree() {
  return http.get<DeptNode[]>('/depts/tree');
}

export function createDept(req: CreateDeptReq) {
  return http.post<{ id: number }>('/depts', req);
}

export function updateDept(id: number, req: UpdateDeptReq) {
  return http.put<void>(`/depts/${id}`, req);
}

export function deleteDept(id: number) {
  return http.delete<void>(`/depts/${id}`);
}
