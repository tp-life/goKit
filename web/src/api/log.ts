import { http } from './request';
import type { OpLog, PageData, PageQuery } from './types';

export function listLogs(params: PageQuery) {
  return http.get<PageData<OpLog>>('/logs', params);
}
