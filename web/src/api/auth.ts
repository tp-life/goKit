import { http } from './request';
import type { ChangePwdReq, LoginData, LoginReq, Profile } from './types';

export function login(req: LoginReq) {
  return http.post<LoginData>('/auth/login', req);
}

export function getProfile() {
  return http.get<Profile>('/auth/profile');
}

export function changePassword(req: ChangePwdReq) {
  return http.put<void>('/auth/password', req);
}
