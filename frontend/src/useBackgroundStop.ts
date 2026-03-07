import { ref, onMounted, onUnmounted } from "vue";
import { useSettings } from "./useSettings";
import { useToast } from "./useToast";

type StopCallback = () => void;
type ResumeCallback = () => void;

export function useBackgroundStop() {
  const { settings } = useSettings();
  const isStopped = ref(false);
  const isInBackground = ref(false);
  let backgroundTimer: number | null = null;
  let stopCallback: StopCallback | null = null;
  let resumeCallback: ResumeCallback | null = null;

  // 开始后台计时
  const startBackgroundTimer = () => {
    if (!settings.enable_background_stop) return;

    // 清除已有的计时器
    if (backgroundTimer) {
      clearTimeout(backgroundTimer);
      backgroundTimer = null;
    }

    // 设置新的计时器
    const delayMs = settings.background_stop_delay * 1000;
    backgroundTimer = window.setTimeout(() => {
      console.log(
        `[BackgroundStop] User has been away for ${settings.background_stop_delay}s, stopping fetch to save resources`,
      );
      isStopped.value = true;
      stopCallback?.();
    }, delayMs);
  };

  // 清除后台计时
  const clearBackgroundTimer = () => {
    if (backgroundTimer) {
      clearTimeout(backgroundTimer);
      backgroundTimer = null;
    }
  };

  // 处理可见性变化
  const handleVisibilityChange = () => {
    if (document.hidden) {
      // 页面进入后台
      console.log("[BackgroundStop] Page entered background");
      isInBackground.value = true;
      startBackgroundTimer();
    } else {
      // 页面回到前台
      console.log("[BackgroundStop] Page became visible");
      isInBackground.value = false;
      clearBackgroundTimer();
      useToast().success(`欢迎回来！`);
      // 如果之前已经停止，则恢复
      if (isStopped.value) {
        console.log("[BackgroundStop] Resuming fetch");
        isStopped.value = false;
        resumeCallback?.();
      }
    }
  };

  // 处理页面冻结（某些浏览器会冻结后台页面）
  const handleFreeze = () => {
    console.log("[BackgroundStop] Page frozen by browser");
    isInBackground.value = true;
    if (settings.enable_background_stop) {
      isStopped.value = true;
      stopCallback?.();
    }
  };

  // 注册回调函数
  const registerCallbacks = (
    onStop: StopCallback,
    onResume: ResumeCallback,
  ) => {
    stopCallback = onStop;
    resumeCallback = onResume;
  };

  // 手动停止（用于外部调用）
  const stop = () => {
    if (!isStopped.value) {
      isStopped.value = true;
      stopCallback?.();
    }
  };

  // 手动恢复（用于外部调用）
  const resume = () => {
    if (isStopped.value) {
      isStopped.value = false;
      resumeCallback?.();
    }
  };

  onMounted(() => {
    // 监听页面可见性变化
    document.addEventListener("visibilitychange", handleVisibilityChange);

    // 监听页面冻结事件（如果浏览器支持）
    if ("onfreeze" in window) {
      window.addEventListener("freeze", handleFreeze);
    }
  });

  onUnmounted(() => {
    // 清理事件监听
    document.removeEventListener("visibilitychange", handleVisibilityChange);
    if ("onfreeze" in window) {
      window.removeEventListener("freeze", handleFreeze);
    }

    // 清除计时器
    clearBackgroundTimer();
  });

  return {
    isStopped,
    isInBackground,
    registerCallbacks,
    stop,
    resume,
  };
}
