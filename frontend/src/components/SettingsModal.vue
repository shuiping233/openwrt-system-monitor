<script setup lang="ts">
import { watch } from 'vue';
import { useDatabase } from '../useDatabase';
import { useToast } from '../useToast';
import { useSettings, type Settings } from '../useSettings';

// 定义 Props (支持 v-model)
const props = defineProps<{
  isOpen: boolean;
}>();

const emit = defineEmits<{
  (e: 'update:isOpen', value: boolean): void;
}>();

// 逻辑
const { clearHistory } = useDatabase();
const { success } = useToast();
const { settings, setConfig } = useSettings();

// 保存配置 (即时生效)
const handleSave = async <K extends keyof Settings>(key: K, value: Settings[K]) => {
  await setConfig(key, value);
  success(`设置已更新，"${key}" 已设置为 "${value}"`);
};

const toggleMetricRecord = async () => {
  const newValue = !settings.enable_metric_record;
  await handleSave('enable_metric_record', newValue);
};

const updateRetentionDays = async (event: Event) => {
  const target = event.target as HTMLInputElement;
  const value = parseInt(target.value, 10);
  if (!isNaN(value) && value >= 1 && value <= 365) {
    await handleSave('retention_days', value);
  }
};

// 清空数据
const handleClear = async () => {
  if (confirm('警告：确定清空所有历史图表数据吗？此操作不可恢复。')) {
    await clearHistory();
    success('历史数据已清空');
  }
};

// ESC 键关闭
const handleKeydown = (e: KeyboardEvent) => {
  if (props.isOpen && e.key === 'Escape') {
    emit('update:isOpen', false);
  }
};

// 监听打开状态，注册/注销全局键盘事件
watch(() => props.isOpen, (newVal) => {
  if (newVal) {
    window.addEventListener('keydown', handleKeydown);
  } else {
    window.removeEventListener('keydown', handleKeydown);
  }
});
</script>

<template>
  <!-- 遮罩层 (无高斯模糊，半透明黑色背景) -->
  <Transition enter-active-class="transition-opacity duration-200 ease-out" enter-from-class="opacity-0"
    enter-to-class="opacity-100" leave-active-class="transition-opacity duration-200 ease-in"
    leave-from-class="opacity-100" leave-to-class="opacity-0">
    <div v-if="isOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-none"
      @click="emit('update:isOpen', false)">
      <!-- 模态框主体 -->
      <div
        class="bg-slate-800 rounded-xl border border-slate-700 w-full max-w-lg shadow-2xl relative transform transition-all max-h-[70vh] flex flex-col"
        @click.stop>

        <!-- 头部: 标题 + 关闭按钮 (右上角 X) -->
        <div class="flex justify-between items-center px-6 py-4 border-b border-slate-700 shrink-0">
          <h2 class="text-xl font-bold text-white">系统设置</h2>
          <button @click="emit('update:isOpen', false)"
            class="text-slate-400 hover:text-white transition-colors p-1 rounded hover:bg-slate-700" title="关闭 (Esc)">
            <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
            </svg>
          </button>
        </div>

        <!-- 内容区域 (可滚动) -->
        <div class="px-6 py-6 space-y-6 overflow-y-auto">

          <!-- 分类 1: 历史数据监控 -->
          <div>
            <h3 class="text-lg font-bold text-blue-400 mb-1 pb-2 border-b border-slate-700">
              历史数据监控
            </h3>



            <div class="mt-4 space-y-4">
              <!-- 配置 : 启用数据保存功能 -->
              <div class="flex justify-between items-center">
                <label class="text-slate-300 text-sm">启用历史图表数据记录</label>
                <button type="button" @click="toggleMetricRecord"
                  class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors"
                  :class="settings.enable_metric_record ? 'bg-blue-600' : 'bg-slate-600'">
                  <span class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform"
                    :class="settings.enable_metric_record ? 'translate-x-6' : 'translate-x-1'" />
                </button>
              </div>
              <!-- 配置 : 数据保存天数 -->
              <div class="flex justify-between items-center">
                <label class="text-sm" :class="settings.enable_metric_record ? 'text-slate-300' : 'text-slate-500'">
                  数据保留天数
                </label>
                <input type="number" min="1" max="365" :value="settings.retention_days" @change="updateRetentionDays"
                  :disabled="!settings.enable_metric_record"
                  class="border rounded px-3 py-1.5 w-24 outline-none transition-colors" :class="settings.enable_metric_record
                    ? 'bg-slate-900 border-slate-600 text-white focus:border-blue-500'
                    : 'bg-slate-800 border-slate-700 text-slate-500 cursor-not-allowed'
                    " />
              </div>
              <p class="text-xs text-slate-500">
                超过此天数的历史图表数据将被自动清理。
              </p>

              <!-- 配置 : 清空所有数据 -->
              <div>
                <button @click="handleClear"
                  class="w-full mt-2 bg-red-600 hover:bg-red-500 text-white py-2 rounded transition-colors text-sm font-semibold flex items-center justify-center gap-2">
                  <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" fill="currentColor" class="bi bi-trash"
                    viewBox="0 0 16 16">
                    <path
                      d="M5.5 5.5A.5.5 0 0 1 6 6v6a.5.5 0 0 1-1 0V6a.5.5 0 0 1 .5-.5m2.5 0a.5.5 0 0 1 .5.5v6a.5.5 0 0 1-1 0V6a.5.5 0 0 1 .5-.5m3 .5a.5.5 0 0 0-1 0v6a.5.5 0 0 0 1 0z" />
                    <path
                      d="M14.5 3a1 1 0 0 1-1 1H13v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V4h-.5a1 1 0 0 1-1-1V2a1 1 0 0 1 1-1H6a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1h3.5a1 1 0 0 1 1 1zM4.118 4 4 4.059V13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V4.059L11.882 4zM2.5 3h11V2h-11z" />
                  </svg>
                  清空所有数据
                </button>
              </div>
            </div>
          </div>

          <!-- DNS 查询设置 -->
          <div>
            <h3 class="text-lg font-bold text-green-400 mb-1 pb-2 border-b border-slate-700">
              DNS 查询设置
            </h3>

            <div class="mt-4 space-y-4">
              <!-- 配置 : DNS 缓存过期时间 -->
              <div class="flex justify-between items-center">
                <label class="text-slate-300 text-sm">DNS 缓存过期时间</label>
                <div class="flex items-center gap-2">
                  <input type="number" min="1" max="60" :value="settings.dns_cache_ttl"
                    @change="(e: Event) => handleSave('dns_cache_ttl', parseInt((e.target as HTMLInputElement).value, 10))"
                    class="border rounded px-3 py-1.5 w-20 outline-none transition-colors bg-slate-900 border-slate-600 text-white focus:border-blue-500" />
                  <span class="text-slate-400 text-sm">分钟</span>
                </div>
              </div>
              <p class="text-xs text-slate-500">
                DNS 查询结果的缓存时间，过期后将重新查询。
              </p>

              <!-- 配置 : DNS 批量查询大小 -->
              <div class="flex justify-between items-center">
                <label class="text-slate-300 text-sm">每批查询 IP 数量</label>
                <input type="number" min="10" max="100" :value="settings.dns_batch_size"
                  @change="(e: Event) => handleSave('dns_batch_size', parseInt((e.target as HTMLInputElement).value, 10))"
                  class="border rounded px-3 py-1.5 w-24 outline-none transition-colors bg-slate-900 border-slate-600 text-white focus:border-blue-500" />
              </div>
              <p class="text-xs text-slate-500">
                每次 DNS 查询请求携带的最大 IP 地址数量。
              </p>

              <!-- 配置 : DNS 轮询间隔 -->
              <div class="flex justify-between items-center">
                <label class="text-slate-300 text-sm">DNS 轮询间隔</label>
                <div class="flex items-center gap-2">
                  <input type="number" min="5" max="300" :value="settings.dns_poll_interval"
                    @change="(e: Event) => handleSave('dns_poll_interval', parseInt((e.target as HTMLInputElement).value, 10))"
                    class="border rounded px-3 py-1.5 w-20 outline-none transition-colors bg-slate-900 border-slate-600 text-white focus:border-blue-500" />
                  <span class="text-slate-400 text-sm">秒</span>
                </div>
              </div>
              <p class="text-xs text-slate-500">
                DNS 查询的轮询间隔时间，开启 DNS 查询后每隔此时间会批量查询一次 IP 对应的主机名。
              </p>
            </div>
          </div>

          <!-- 后台运行设置 -->
          <div>
            <h3 class="text-lg font-bold text-orange-400 mb-1 pb-2 border-b border-slate-700">
              后台运行设置
            </h3>

            <div class="mt-4 space-y-4">
              <!-- 配置 : 启用后台停止 -->
              <div class="flex justify-between items-center">
                <label class="text-slate-300 text-sm">后台自动停止刷新</label>
                <button type="button" @click="handleSave('enable_background_stop', !settings.enable_background_stop)"
                  class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors"
                  :class="settings.enable_background_stop ? 'bg-orange-600' : 'bg-slate-600'">
                  <span class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform"
                    :class="settings.enable_background_stop ? 'translate-x-6' : 'translate-x-1'" />
                </button>
              </div>
              <p class="text-xs text-slate-500">
                当浏览器标签页切换到后台或最小化时，自动停止数据刷新以节省路由器资源。
              </p>

              <!-- 配置 : 后台停止延迟 -->
              <div class="flex justify-between items-center">
                <label class="text-sm" :class="settings.enable_background_stop ? 'text-slate-300' : 'text-slate-500'">
                  后台停止延迟
                </label>
                <div class="flex items-center gap-2">
                  <input type="number" min="10" max="600" :value="settings.background_stop_delay"
                    @change="(e: Event) => handleSave('background_stop_delay', parseInt((e.target as HTMLInputElement).value, 10))"
                    :disabled="!settings.enable_background_stop"
                    class="border rounded px-3 py-1.5 w-20 outline-none transition-colors" :class="settings.enable_background_stop
                      ? 'bg-slate-900 border-slate-600 text-white focus:border-blue-500'
                      : 'bg-slate-800 border-slate-700 text-slate-500 cursor-not-allowed'
                      " />
                  <span class="text-slate-400 text-sm">秒</span>
                </div>
              </div>
              <p class="text-xs text-slate-500">
                页面进入后台后等待多久才停止刷新，避免用户短暂切换时频繁启停。
              </p>
            </div>
          </div>

        </div>

        <!-- 底部: 退出按钮 (右下角) -->
        <div class="px-6 py-4 border-t border-slate-700 flex justify-end bg-slate-800/50 rounded-b-xl flex-shrink-0">
          <button @click="emit('update:isOpen', false)"
            class="px-6 py-2 bg-slate-700 hover:bg-slate-600 text-white rounded transition-colors text-sm font-medium">
            退出
          </button>
        </div>

      </div>
    </div>
  </Transition>
</template>

<style scoped>
/* 确保过渡动画流畅 */
</style>