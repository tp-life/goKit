import type { MessageInstance } from 'antd/es/message/interface';

// request 层运行在 React 组件之外，这里用一个由 <App/> 注册的 message 实例做全局提示
let holder: MessageInstance | null = null;

export function registerMessage(api: MessageInstance) {
  holder = api;
}

export const notify = {
  error(msg: string) {
    if (holder) holder.error(msg);
    else window.alert(msg);
  },
  success(msg: string) {
    holder?.success(msg);
  },
};
