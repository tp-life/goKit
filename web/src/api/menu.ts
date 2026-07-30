import { http } from './request';
import type { CreateMenuReq, MenuNode, UpdateMenuReq } from './types';

export function getMenuTree() {
  return http.get<MenuNode[]>('/menus/tree');
}

/** 当前登录用户可见的菜单树（驱动侧边栏） */
export function getMyMenus() {
  return http.get<MenuNode[]>('/menus/mine');
}

export function createMenu(req: CreateMenuReq) {
  return http.post<{ id: number }>('/menus', req);
}

export function updateMenu(id: number, req: UpdateMenuReq) {
  return http.put<void>(`/menus/${id}`, req);
}

export function deleteMenu(id: number) {
  return http.delete<void>(`/menus/${id}`);
}
