import { ref } from "vue";

// 定义 Toast 数据结构
interface ToastItem {
  id: number;
  message: string;
  type: "success" | "error";
}

// 内部状态（单例模式，整个应用共享）
const toasts = ref<ToastItem[]>([]);

export function useToast() {
  /**
   * 添加一条 Toast
   * @param message 消息内容
   * @param type 类型 'success' | 'error'，默认 success
   * @param duration 持续时间，默认 3000ms
   */
  const showToast = (message: string, type: "success" | "error" = "success", duration = 3000) => {
    const id = Date.now();
    toasts.value.push({ id, message, type });

    // 自动移除
    setTimeout(() => {
      removeToast(id);
    }, duration);
  };

  const removeToast = (id: number) => {
    const index = toasts.value.findIndex((t) => t.id === id);
    if (index !== -1) {
      toasts.value.splice(index, 1);
    }
  };

  return {
    list: toasts,
    show: showToast,
    success: (msg: string) => showToast(msg, "success"),
    error: (msg: string) => showToast(msg, "error"),
    removeToast,
  };
}
