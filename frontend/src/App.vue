<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted, watch, type Ref } from "vue";
import dayjs from "dayjs";
import type {
  DynamicApiResponse,
  StaticApiResponse,
  ConnectionApiResponse,
  AggregationTrafficResponse,
} from "./model";
import { APP_CONFIG } from "./config/app";
import SettingsModal from "./components/SettingsModal.vue";
import NetworkConnectionTable from "./components/NetworkConnectionTable.vue";
import SystemOverview from "./components/SystemOverview.vue";
import MonitoringCharts from "./components/MonitoringCharts.vue";
import { useToast } from "./useToast";
import Toaster from "./components/Toaster.vue";
import { useDatabase } from "./useDatabase";
import { covertDataBytes, normalizeToBytes } from "./utils/convert";
import type { HistoryRecord } from "./model";
import { useSettings, type TabType } from "./useSettings";
import { useBackgroundStop } from "./useBackgroundStop";

const { addHistoryBatch } = useDatabase();
const { settings, setConfig, init: initSettings } = useSettings();
const { registerCallbacks, isStopped } = useBackgroundStop();

const data = reactive({
  dynamic: {} as DynamicApiResponse,
  static: {} as StaticApiResponse,
  connection: {} as ConnectionApiResponse,
  aggregation: {} as AggregationTrafficResponse,
});

const showSettings = ref(false);

const uiState = reactive({
  lastUpdated: "--",
  isLoading: false,
  refreshInterval: 2000,
  status: "初始化",
});

let timer: Ref<number | null> = ref(null);

// ================= 3. 辅助函数 =================

const formatTime = (): string => {
  return dayjs().format("YYYY-MM-DD HH:mm:ss");
};

// 状态颜色映射
const getStatusColor = (status: string): string => {
  switch (status) {
    case "运行中":
      return "#10b981"; // 绿色
    case "刷新中":
      return "#3b82f6"; // 蓝色
    case "错误":
      return "#ef4444"; // 红色
    default:
      return "#94a3b8"; // 灰色
  }
};

// ================= 数据存储逻辑 =================

/**
 * 将动态数据保存到数据库
 */
const saveDynamicDataToDB = (
  dynamicData: DynamicApiResponse,
  connectionData: ConnectionApiResponse,
) => {
  if (!dynamicData) return;
  const now = Date.now();
  const records: Omit<HistoryRecord, "id">[] = [];

  // CPU 使用率
  const cpuUsage = dynamicData.cpu?.total?.usage;
  if (cpuUsage?.value !== undefined) {
    records.push({
      timestamp: now,
      metric: "cpu_total",
      value: cpuUsage.value,
      unit: cpuUsage.unit,
      label: "total",
    });
  }

  // 1. CPU Temp: 平均值
  if (dynamicData.cpu) {
    let totalTemp = 0,
      count = 0;
    Object.values(dynamicData.cpu).forEach((c: any) => {
      if (c.temperature.value > 0) {
        totalTemp += c.temperature.value;
        count++;
      }
    });
    if (count > 0) {
      const unit = Object.values(dynamicData.cpu)[0].temperature.unit;
      records.push({
        timestamp: now,
        metric: "cpu_temp",
        value: totalTemp / count,
        unit: unit,
        label: "average",
      });
    }
  }

  // 3. Memory: 内存使用量和百分比
  if (dynamicData.memory) {
    if (dynamicData.memory.used) {
      const [value, unit] = covertDataBytes(
        dynamicData.memory.used.value,
        dynamicData.memory.used.unit,
        dynamicData.memory.total.unit,
      );
      records.push({
        timestamp: now,
        metric: "memory_used",
        value: value,
        unit: unit,
        label: "used",
      });
    }
    if (dynamicData.memory.used_percent) {
      records.push({
        timestamp: now,
        metric: "memory_used_percent",
        value: dynamicData.memory.used_percent.value,
        unit: dynamicData.memory.used_percent.unit,
        label: "percent",
      });
    }
  }

  // 4. Network: 总网卡 IO 和 pppoe-wan IO
  if (dynamicData.network?.total) {
    const totalIn = dynamicData.network.total.incoming;
    const totalOut = dynamicData.network.total.outgoing;
    records.push({
      timestamp: now,
      metric: "network_in",
      value: totalIn.value,
      unit: totalIn.unit,
      label: "total-down",
    });
    records.push({
      timestamp: now,
      metric: "network_out",
      value: totalOut.value,
      unit: totalOut.unit,
      label: "total-up",
    });
  }

  if (dynamicData.network?.["pppoe-wan"]) {
    const pppoeIn = dynamicData.network["pppoe-wan"].incoming;
    const pppoeOut = dynamicData.network["pppoe-wan"].outgoing;
    records.push({
      timestamp: now,
      metric: "network_in",
      value: pppoeIn.value,
      unit: pppoeIn.unit,
      label: "pppoe-wan-down",
    });
    records.push({
      timestamp: now,
      metric: "network_out",
      value: pppoeOut.value,
      unit: pppoeOut.unit,
      label: "pppoe-wan-up",
    });
  }

  // 5. Storage: 总 IO
  if (dynamicData.storage) {
    let totalBytes = 0;
    Object.values(dynamicData.storage).forEach((d: any) => {
      const readBytes = normalizeToBytes(d.read.value, d.read.unit);
      const writeBytes = normalizeToBytes(d.write.value, d.write.unit);
      if (readBytes > 0) totalBytes += readBytes;
      if (writeBytes > 0) totalBytes += writeBytes;
    });
    records.push({
      timestamp: now,
      metric: "storage_total_io",
      value: totalBytes,
      unit: "B/S",
      label: "total",
    });
  }

  // 6. Connections: 4条记录 (Total, TCP, UDP, Other)
  if (connectionData?.counts) {
    const counts = connectionData.counts;
    records.push({
      timestamp: now,
      metric: "connections",
      value: counts.tcp,
      unit: "count",
      label: "TCP",
    });
    records.push({
      timestamp: now,
      metric: "connections",
      value: counts.udp,
      unit: "count",
      label: "UDP",
    });
    records.push({
      timestamp: now,
      metric: "connections",
      value: counts.other,
      unit: "count",
      label: "Other",
    });
    records.push({
      timestamp: now,
      metric: "connections",
      value: counts.tcp + counts.udp + counts.other,
      unit: "count",
      label: "Total",
    });
  }

  if (records.length > 0) {
    addHistoryBatch(records).catch(console.error);
  }
};

// ================= 4. 核心逻辑 =================

const fetchData = async () => {
  uiState.isLoading = true;
  uiState.status = "刷新中";
  const reqTime = formatTime();

  const shouldFetchAll =
    settings.enable_metric_record || settings.active_tab === "monitoringCharts";
  const shouldFetchDynamic = shouldFetchAll || settings.active_tab === "system";
  const shouldFetchConnection = shouldFetchAll || settings.active_tab === "network";
  const shouldFetchStatic = shouldFetchAll || settings.active_tab === "system";
  const shouldFetchAggregation = shouldFetchAll || settings.active_tab === "network";

  try {
    const requests: Promise<Response>[] = [];

    if (shouldFetchDynamic) requests.push(fetch("/metric/dynamic"));
    if (shouldFetchConnection) requests.push(fetch("/metric/network_connection"));
    if (shouldFetchStatic) requests.push(fetch("/metric/static"));
    if (shouldFetchAggregation) requests.push(fetch("/metric/aggregation_traffic"));

    const responses = await Promise.all(requests);

    let resIndex = 0;

    if (shouldFetchDynamic) {
      const dRes = responses[resIndex++];
      if (!dRes.ok) throw new Error(`动态数据接口错误: ${dRes.status} ${dRes.statusText}`);
      data.dynamic = (await dRes.json()) as DynamicApiResponse;
    }

    if (shouldFetchConnection) {
      const cRes = responses[resIndex++];
      if (!cRes.ok) throw new Error(`网络连接接口错误: ${cRes.status} ${cRes.statusText}`);
      data.connection = (await cRes.json()) as ConnectionApiResponse;
    }

    if (shouldFetchStatic) {
      const sRes = responses[resIndex++];
      if (!sRes.ok) throw new Error(`静态数据接口错误: ${sRes.status} ${sRes.statusText}`);
      data.static = (await sRes.json()) as StaticApiResponse;
    }

    if (shouldFetchAggregation) {
      const aRes = responses[resIndex];
      if (!aRes.ok) throw new Error(`聚合流量接口错误: ${aRes.status} ${aRes.statusText}`);
      data.aggregation = (await aRes.json()) as AggregationTrafficResponse;
    }

    if (settings.enable_metric_record) {
      saveDynamicDataToDB(data.dynamic, data.connection);
    }

    uiState.status = "运行中";
  } catch (e) {
    console.error(e);
    uiState.status = "错误";
    const { error } = useToast();

    if (e instanceof TypeError) {
      error(`网络错误: ${e.message}`);
    } else if (e.message.includes("接口错误")) {
      error(e.message);
    } else {
      error(`请求失败: ${e.message}`);
    }
  } finally {
    uiState.isLoading = false;
    uiState.lastUpdated = reqTime;
  }
};

const startPolling = (showToast = false) => {
  if (timer.value) clearInterval(timer.value);
  timer.value = window.setInterval(() => {
    // 只有在未停止状态下才执行 fetch
    if (!isStopped.value) {
      fetchData();
    }
  }, uiState.refreshInterval);
  if (showToast) {
    const { success } = useToast();
    success(`刷新间隔已调整为 ${uiState.refreshInterval / 1000} 秒`);
  }
};

const stopPolling = () => {
  if (timer.value) {
    clearInterval(timer.value);
    timer.value = null;
  }
};

const handleRefreshIntervalChange = () => {
  setConfig("refresh_interval", uiState.refreshInterval);
  startPolling(true);
};

const handleTabChange = (tab: TabType) => {
  setConfig("active_tab", tab);
  if (settings.enable_metric_record === false) {
    fetchData();
  }
};

// ================= 5. 生命周期 =================

onMounted(async () => {
  await initSettings();
  uiState.refreshInterval = settings.refresh_interval;

  // 注册后台停止回调
  registerCallbacks(
    () => {
      // 停止时的回调
      console.log("[App] Background stop triggered, pausing data fetch");
      uiState.status = "已暂停(后台)";
    },
    () => {
      // 恢复时的回调
      console.log("[App] Resuming from background stop");
      fetchData(); // 立即刷新一次数据
      startPolling();
      uiState.status = "运行中";
    },
  );

  fetchData();
  startPolling();
});

onUnmounted(() => {
  stopPolling();
});
</script>

<template>
  <!-- 整个应用容器 -->
  <div class="max-auto mx-auto p-5 bg-slate-900 text-slate-50 min-h-screen">
    <!-- Header -->
    <header class="flex justify-between items-center mb-8 pb-5 border-b border-slate-700">
      <div class="flex items-center gap-2">
        <svg
          width="24"
          height="24"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
        </svg>
        <span class="text-xl font-bold">{{ APP_CONFIG.title }}</span>
      </div>

      <div class="flex items-center gap-2 text-sm text-slate-400">
        <!-- Status Dot -->
        <div
          :style="{
            width: '8px',
            height: '8px',
            borderRadius: '50%',
            background: getStatusColor(uiState.status),
            boxShadow: `0 0 8px ${getStatusColor(uiState.status)}`,
          }"
        ></div>
        <span>{{ uiState.status }}</span>
        <!-- Spinner: Using Tailwind animate-spin -->
        <!-- <div v-if="uiState.isLoading"
          class="w-3.5 h-3.5 border-2 border-slate-500 border-t-white rounded-full animate-spin"></div> -->

        <span class="font-mono">{{ uiState.lastUpdated }}</span>

        <!-- Select -->
        <select
          v-model.number="uiState.refreshInterval"
          @change="handleRefreshIntervalChange"
          class="bg-slate-800 text-white border border-slate-700 rounded px-2 py-1 outline-none focus:border-slate-500 cursor-pointer"
        >
          <option :value="1000">1s</option>
          <option :value="2000">2s</option>
          <option :value="3000">3s</option>
          <option :value="5000">5s</option>
          <option :value="10000">10s</option>
          <option :value="30000">30s</option>
        </select>

        <!-- 设置齿轮按钮 -->
        <button
          @click="showSettings = true"
          class="text-slate-400 hover:text-white transition-colors"
          title="设置"
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="24"
            height="24"
            fill="currentColor"
            class="bi bi-gear"
            viewBox="0 0 16 16"
          >
            <path
              d="M8 4.754a3.246 3.246 0 1 0 0 6.492 3.246 3.246 0 0 0 0-6.492M5.754 8a2.246 2.246 0 1 1 4.492 0 2.246 2.246 0 0 1-4.492 0"
            />
            <path
              d="M9.796 1.343c-.527-1.79-3.065-1.79-3.592 0l-.094.319a.873.873 0 0 1-1.255.52l-.292-.16c-1.64-.892-3.433.902-2.54 2.541l.159.292a.873.873 0 0 1-.52 1.255l-.319.094c-1.79.527-1.79 3.065 0 3.592l.319.094a.873.873 0 0 1 .52 1.255l-.16.292c-.892 1.64.901 3.434 2.541 2.54l.292-.159a.873.873 0 0 1 1.255.52l.094.319c.527 1.79 3.065 1.79 3.592 0l.094-.319a.873.873 0 0 1 1.255-.52l.292.16c1.64.893 3.434-.902 2.54-2.541l-.159-.292a.873.873 0 0 1 .52-1.255l.319-.094c1.79-.527 1.79-3.065 0-3.592l-.319-.094a.873.873 0 0 1-.52-1.255l.16-.292c.893-1.64-.902-3.433-2.541-2.54l-.292.159a.873.873 0 0 1-1.255-.52zm-2.633.283c.246-.835 1.428-.835 1.674 0l.094.319a1.873 1.873 0 0 0 2.693 1.115l.291-.16c.764-.415 1.6.42 1.184 1.185l-.159.292a1.873 1.873 0 0 0 1.116 2.692l.318.094c.835.246.835 1.428 0 1.674l-.319.094a1.873 1.873 0 0 0-1.115 2.693l.16.291c.415.764-.42 1.6-1.185 1.184l-.291-.159a1.873 1.873 0 0 0-2.693 1.116l-.094.318c-.246.835-1.428.835-1.674 0l-.094-.319a1.873 1.873 0 0 0-2.692-1.115l-.292.16c-.764.415-1.6-.42-1.184-1.185l.159-.291A1.873 1.873 0 0 0 1.945 8.93l-.319-.094c-.835-.246-.835-1.428 0-1.674l.319-.094A1.873 1.873 0 0 0 3.06 4.377l-.16-.292c-.415-.764.42-1.6 1.185-1.184l.292.159a1.873 1.873 0 0 0 2.692-1.115z"
            />
          </svg>
        </button>
      </div>
    </header>

    <!-- Tabs -->
    <nav class="flex gap-2 mb-5">
      <button
        @click="handleTabChange('system')"
        class="px-5 py-2 text-sm font-semibold cursor-pointer border border-slate-700 rounded-lg transition-colors"
        :class="[
          settings.active_tab === 'system'
            ? 'text-white border-b-2 border-blue-500 bg-transparent'
            : 'text-slate-400 bg-slate-800/50 hover:bg-slate-800',
        ]"
      >
        系统概览
      </button>
      <button
        @click="handleTabChange('network')"
        class="px-5 py-2 text-sm font-semibold cursor-pointer border border-slate-700 rounded-lg transition-colors"
        :class="[
          settings.active_tab === 'network'
            ? 'text-white border-b-2 border-blue-500 bg-transparent'
            : 'text-slate-400 bg-slate-800/50 hover:bg-slate-800',
        ]"
      >
        网络连接
      </button>
      <button
        @click="handleTabChange('monitoringCharts')"
        class="px-5 py-2 text-sm font-semibold cursor-pointer border border-slate-700 rounded-lg transition-colors"
        :class="[
          settings.active_tab === 'monitoringCharts'
            ? 'text-white border-b-2 border-blue-500 bg-transparent'
            : 'text-slate-400 bg-slate-800/50 hover:bg-slate-800',
        ]"
      >
        监控图表
      </button>
    </nav>

    <!-- Tab: System Overview -->
    <div v-if="settings.active_tab === 'system'">
      <SystemOverview :data="data" />
    </div>

    <!-- Tab: Network Connections -->
    <div v-if="settings.active_tab === 'network'" class="p-0">
      <NetworkConnectionTable
        :connection-data="data.connection"
        :aggregation-data="data.aggregation"
      />
    </div>
    <!-- Tab: Analytics -->
    <div v-if="settings.active_tab === 'monitoringCharts'">
      <MonitoringCharts :data="data" />
    </div>

    <SettingsModal v-model:isOpen="showSettings" />

    <Toaster />
  </div>
</template>

<style></style>
