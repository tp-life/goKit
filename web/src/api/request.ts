import { notify } from '../utils/notify';

export const TOKEN_KEY = 'gokit_token';

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  code: number;
  constructor(code: number, message: string) {
    super(message);
    this.code = code;
  }
}

interface Shell<T> {
  code: number;
  message: string;
  data?: T;
}

type Params = Record<string, string | number | boolean | undefined | null>;

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  params?: Params,
): Promise<T> {
  let url = `/api/v1${path}`;
  if (params) {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null && v !== '') qs.set(k, String(v));
    }
    const s = qs.toString();
    if (s) url += `?${s}`;
  }

  const headers: Record<string, string> = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const token = getToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(url, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    notify.error('网络异常，请稍后重试');
    throw new ApiError(-1, 'network error');
  }

  let json: Shell<T> | null = null;
  try {
    json = (await res.json()) as Shell<T>;
  } catch {
    json = null;
  }

  const code = json?.code ?? (res.ok ? 0 : res.status * 100);

  // 未认证：清 token 并跳登录页（登录页自身除外）
  if (res.status === 401 || code === 40100) {
    clearToken();
    notify.error(json?.message || '未登录或登录已过期');
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
    throw new ApiError(code, json?.message || 'unauthorized');
  }

  if (!res.ok || !json || code !== 0) {
    const msg = json?.message || `请求失败（HTTP ${res.status}）`;
    notify.error(msg);
    throw new ApiError(code, msg);
  }

  return json.data as T;
}

export const http = {
  get: <T>(path: string, params?: Params) => request<T>('GET', path, undefined, params),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  delete: <T>(path: string) => request<T>('DELETE', path),
};
