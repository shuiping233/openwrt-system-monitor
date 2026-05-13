<script setup lang="ts">
import { ref, reactive, onMounted, watch, nextTick, computed } from 'vue';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { LineChart } from 'echarts/charts';
import {
  TitleComponent,
  TooltipComponent,
  GridComponent,
  ToolboxComponent,
  LegendComponent
} from 'echarts/components';
import type { EChartsOption } from 'echarts';
import { useDatabase } from '../useDatabase';
import type { DynamicApiResponse, StorageData } from '../model';
import { TimeRanges } from '../model';
import { normalizeToBytes, formatIOBytes, formatMetric, covertDataBytes } from '../utils/convert';
import { useSettings } from '../useSettings';

// 注册 ECharts 组件
use([
  CanvasRenderer,
  LineChart,
  TitleComponent,
  TooltipComponent,
  GridComponent,
  ToolboxComponent,
  LegendComponent
]);

// Props
const props = defineProps<{
  data: {
    dynamic: DynamicApiResponse,
    static: any,
    connection: any
  };
}>();

const { getHistory, getAccordionState, setAccordionState } = useDatabase();
const { settings, setConfig } = useSettings();

// ================= 常量与辅助函数 =================
// 颜色配置
const colors = [
  '#10b981', '#3b82f6', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4', '#f97316',
  '#84cc16', '#14b8a6', '#6366f1', '#f43f5e', '#0ea5e9', '#d946ef', '#22c55e', '#e11d48'
];

// ================= 状态定义 =================
const globalTimeRange = computed(() => settings.chart_time_range);

const chartStates = reactive<Record<string, { range: number; targetUnit?: string }>>({});

// 折叠面板状态
const uiState = reactive({
  accordions: {
    basic: true,
    cpu: true,
    memory: true,
    network: true,
    storage: true
  }
});

// 加载折叠状态
onMounted(async () => {
  for (const key of Object.keys(uiState.accordions)) {
    const state = await getAccordionState(`charts_${key}`);
    if (state !== undefined) {
      uiState.accordions[key] = state;
    }
  }
});

// 切换折叠状态
const toggleAccordion = async (key: string) => {
  uiState.accordions[key] = !uiState.accordions[key];
  await setAccordionState(`charts_${key}`, uiState.accordions[key]);
};

// ================= ECharts Option 生成 =================

// 百分比/温度图表 (Y轴固定单位)
function getFixedAxisOption(title: string, color: string, unit: string, min?: number, max?: number): EChartsOption {
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(30, 41, 59, 0.9)',
      textStyle: { color: '#fff' },
      formatter: (params: any) => {
        const param = params[0];
        return `${param.seriesName}<br/>${new Date(param.value[0]).toLocaleString()}<br/>${formatMetric(param.value[1], unit)}`;
      }
    },
    grid: { left: 40, right: 20, bottom: 30, top: 60, containLabel: false },
    title: { text: title, textStyle: { color: '#94a3b8', fontSize: 14 }, left: 'center' },
    toolbox: { show: true, feature: { saveAsImage: { show: true, title: '保存图片' } } },
    xAxis: { type: 'time', splitLine: { show: false }, axisLabel: { color: '#64748b' } },
    yAxis: {
      type: 'value',
      min: min, max: max,
      splitLine: { lineStyle: { color: '#334155', type: 'dashed' } },
      axisLabel: { formatter: `{value} ${unit}` }
    },
    series: [{
      type: 'line',
      name: title,
      showSymbol: false,
      data: [],
      lineStyle: { width: 2, color: color },
      areaStyle: { opacity: 0.1, color: color },
      smooth: false
    }]
  };
}

// IO 类图表 (Y轴自动归一化显示，这里接收的是 Bytes/s)
function getIOOption(title: string, color: string): EChartsOption {
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(30, 41, 59, 0.9)',
      textStyle: { color: '#fff' },
      formatter: (params: any) => {
        const param = params[0];
        const displayValue = formatIOBytes(param.value[1]);
        return `${param.seriesName}<br/>${new Date(param.value[0]).toLocaleString()}<br/>${displayValue}`;
      }
    },
    grid: { left: 40, right: 20, bottom: 30, top: 60, containLabel: false },
    title: { text: title, textStyle: { color: '#94a3b8', fontSize: 14 }, left: 'center' },
    toolbox: { show: true, feature: { saveAsImage: { show: true, title: '保存图片' } } },
    xAxis: { type: 'time', splitLine: { show: false }, axisLabel: { color: '#64748b' } },
    yAxis: {
      type: 'value',
      scale: true,
      splitLine: { lineStyle: { color: '#334155', type: 'dashed' } },
      axisLabel: { formatter: (value: number) => formatIOBytes(value) }
    },
    series: [{
      type: 'line',
      name: title,
      showSymbol: false,
      data: [],
      lineStyle: { width: 2, color: color },
      areaStyle: { opacity: 0.1, color: color },
      smooth: false
    }]
  };
}

// 多系列图表（用于多条折线）
function getMultiSeriesOption(title: string, series: any[], legend: boolean = true, isIO: boolean = false): EChartsOption {
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(30, 41, 59, 0.9)',
      textStyle: { color: '#fff' },
      formatter: (params: any) => {
        let result = `<div style="font-weight:bold;margin-bottom:5px;">${new Date(params[0].value[0]).toLocaleString()}</div>`;
        params.forEach((param: any) => {
          let valueStr = param.value[1].toString();
          if (isIO) {
            valueStr = formatIOBytes(param.value[1]);
          }
          result += `<div style="display:flex;align-items:center;margin:3px 0;">
            <span style="display:inline-block;width:10px;height:10px;background:${param.color};margin-right:8px;border-radius:2px;"></span>
            <span style="color:#94a3b8;margin-right:8px;">${param.seriesName}:</span>
            <span style="color:#fff;font-weight:bold;">${valueStr}</span>
          </div>`;
        });
        return result;
      }
    },
    grid: { left: 40, right: 20, bottom: 30, top: legend ? 80 : 60, containLabel: false },
    title: { text: title, textStyle: { color: '#94a3b8', fontSize: 14 }, left: 'center' },
    toolbox: { show: true, feature: { saveAsImage: { show: true, title: '保存图片' } } },
    legend: legend ? {
      show: true,
      top: 30,
      textStyle: { color: '#94a3b8' }
    } : undefined,
    xAxis: { type: 'time', splitLine: { show: false }, axisLabel: { color: '#64748b' } },
    yAxis: {
      type: 'value',
      scale: true,
      splitLine: { lineStyle: { color: '#334155', type: 'dashed' } },
      axisLabel: { formatter: (value: number) => isIO ? formatIOBytes(value) : `${value}` }
    },
    series: series
  };
}

// 图表选项组织
const chartOptions = reactive<Record<string, EChartsOption>>({
  // 基本指标
  cpu_total: getFixedAxisOption('CPU 总占用', '#3b82f6', '%', 0, 100),
  cpu_temp: getFixedAxisOption('CPU 温度', '#f59e0b', '°C', 0, 120),
  memory_used_percent: getFixedAxisOption('内存占用比例', '#8b5cf6', '%', 0, 100),
  connections: getMultiSeriesOption('网络连接数', [
    { type: 'line', name: 'TCP', showSymbol: false, data: [], lineStyle: { width: 2, color: '#10b981' }, smooth: false },
    { type: 'line', name: 'UDP', showSymbol: false, data: [], lineStyle: { width: 2, color: '#f59e0b' }, smooth: false },
    { type: 'line', name: 'Other', showSymbol: false, data: [], lineStyle: { width: 2, color: '#64748b' }, smooth: false },
    { type: 'line', name: 'Total', showSymbol: false, data: [], lineStyle: { width: 2, color: '#3b82f6' }, smooth: false }
  ], true, false),

  // 网络分类 - 总网卡 IO
  network_total: getMultiSeriesOption('总网卡 IO', [
    { type: 'line', name: '下行', showSymbol: false, data: [], lineStyle: { width: 2, color: '#10b981' }, areaStyle: { opacity: 0.1, color: '#10b981' }, smooth: false },
    { type: 'line', name: '上行', showSymbol: false, data: [], lineStyle: { width: 2, color: '#f97316' }, areaStyle: { opacity: 0.1, color: '#f97316' }, smooth: false }
  ], true, true),

  // 网络分类 - pppoe-wan IO
  network_pppoe_wan: getMultiSeriesOption('pppoe-wan IO', [
    { type: 'line', name: '下行', showSymbol: false, data: [], lineStyle: { width: 2, color: '#10b981' }, areaStyle: { opacity: 0.1, color: '#10b981' }, smooth: false },
    { type: 'line', name: '上行', showSymbol: false, data: [], lineStyle: { width: 2, color: '#f97316' }, areaStyle: { opacity: 0.1, color: '#f97316' }, smooth: false }
  ], true, true),

  // 存储分类 - 总 IO
  storage_total_io: getIOOption('总磁盘 IO', '#ec4899')

});

const initMemoryUsedChart = (data: DynamicApiResponse) => {
  if (!chartOptions.memory_used && data.memory?.total) {
    chartOptions.memory_used = getFixedAxisOption(
      '内存使用量',
      '#8b5cf6',
      data.memory.total.unit,
      0,
      data.memory.total.value
    );
  }
};

// 计算属性：过滤各分类的图表
const getBasicCharts = computed(() => {
  return Object.entries(chartOptions).filter(([key]) =>
    ['cpu_total', 'cpu_temp', 'memory_used_percent', 'connections', 'network_total', 'network_pppoe_wan', 'storage_total_io'].includes(key)
  );
});

const getCpuCoreCharts = computed(() => {
  return Object.entries(chartOptions).filter(([key]) =>
    key.startsWith('cpu_core_')
  );
});

const getMemoryCharts = computed(() => {
  return Object.entries(chartOptions).filter(([key]) =>
    ['memory_used', 'memory_used_percent'].includes(key)
  );
});

const getNetworkCharts = computed(() => {
  return Object.entries(chartOptions).filter(([key]) =>
    key === 'network_iface_all'
  );
});

const getStorageCharts = computed(() => {
  return Object.entries(chartOptions).filter(([key]) =>
    key.startsWith('storage_io_') || key.startsWith('storage_space_')
  );
});

// ================= 数据加载与处理 =================

function filterDataByTimeRange(data: [number, number][], range: number): [number, number][] {
  const now = Date.now();
  const cutoffTime = now - range;
  return data.filter(([timestamp]) => timestamp >= cutoffTime);
}

const loadHistoryAndRender = async (key: string) => {
  const range = chartStates[key]?.range || globalTimeRange.value;
  const data = await getHistory(key as any, range);

  let seriesData: [number, number][];
  const isIO = ['network_in', 'network_out', 'storage_io', 'storage_total_io'].includes(key);

  if (isIO) {
    seriesData = data.map(item => {
      const normalizedValue = normalizeToBytes(item.value, item.unit);
      return [item.timestamp, normalizedValue] as [number, number];
    });
  } else {
    seriesData = data.map(item => [item.timestamp, item.value] as [number, number]);
  }

  const option = chartOptions[key];
  if (option && (option.series as any)[0]) {
    (option.series as any)[0].data = seriesData;
  }
};

const loadHistoryAndRenderMultiSeries = async (chartKey: string, metric: string, seriesIndex: number, label: string) => {
  const range = chartStates[chartKey]?.range || globalTimeRange.value;
  const data = await getHistory(metric as any, range);

  const seriesData = data
    .filter(item => item.label === label)
    .map(item => [item.timestamp, item.value] as [number, number]);

  const option = chartOptions[chartKey];
  if (option && (option.series as any)[seriesIndex]) {
    (option.series as any)[seriesIndex].data = seriesData;
  }
};

// 加载网络连接数历史数据
const loadConnectionsHistory = async () => {
  const chartKey = 'connections';
  const range = chartStates[chartKey]?.range || globalTimeRange.value;

  const data = await getHistory('connections', range);

  const seriesTcp = data
    .filter(item => item.label === 'TCP')
    .map(item => [item.timestamp, item.value] as [number, number]);

  const seriesUdp = data
    .filter(item => item.label === 'UDP')
    .map(item => [item.timestamp, item.value] as [number, number]);

  const seriesOther = data
    .filter(item => item.label === 'Other')
    .map(item => [item.timestamp, item.value] as [number, number]);

  const seriesTotal = data
    .filter(item => item.label === 'Total')
    .map(item => [item.timestamp, item.value] as [number, number]);

  const option = chartOptions[chartKey];
  if (option && option.series) {
    (option.series as any)[0].data = seriesTcp;
    (option.series as any)[1].data = seriesUdp;
    (option.series as any)[2].data = seriesOther;
    (option.series as any)[3].data = seriesTotal;
  }
};

// 加载总网卡IO历史数据
const loadNetworkTotalHistory = async () => {
  const chartKey = 'network_total';
  const range = chartStates[chartKey]?.range || globalTimeRange.value;

  const dataIn = await getHistory('network_in', range);
  const seriesDown = dataIn
    .filter(item => item.label === 'total-down')
    .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

  const dataOut = await getHistory('network_out', range);
  const seriesUp = dataOut
    .filter(item => item.label === 'total-up')
    .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

  const option = chartOptions[chartKey];
  if (option && option.series) {
    (option.series as any)[0].data = seriesDown;
    (option.series as any)[1].data = seriesUp;
  }
};

// 加载pppoe-wan IO历史数据
const loadNetworkPppoeWanHistory = async () => {
  const chartKey = 'network_pppoe_wan';
  const range = chartStates[chartKey]?.range || globalTimeRange.value;

  const dataIn = await getHistory('network_in', range);
  const seriesDown = dataIn
    .filter(item => item.label === 'pppoe-wan-down')
    .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

  const dataOut = await getHistory('network_out', range);
  const seriesUp = dataOut
    .filter(item => item.label === 'pppoe-wan-up')
    .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

  const option = chartOptions[chartKey];
  if (option && option.series) {
    (option.series as any)[0].data = seriesDown;
    (option.series as any)[1].data = seriesUp;
  }
};

// 加载各网卡IO历史数据
const loadNetworkIfaceHistory = async () => {
  const chartKey = 'network_iface_all';
  const range = chartStates[chartKey]?.range || globalTimeRange.value;

  if (!props.data.dynamic?.network) return;

  const interfaces = Object.keys(props.data.dynamic.network).filter(k => k !== 'total');
  const series: any[] = [];

  const dataIn = await getHistory('network_in', range);
  const dataOut = await getHistory('network_out', range);

  interfaces.forEach((iface, idx) => {
    const colorIdx = idx * 2;
    const seriesDown = dataIn
      .filter(item => item.label === `${iface}-down`)
      .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

    const seriesUp = dataOut
      .filter(item => item.label === `${iface}-up`)
      .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

    series.push({
      type: 'line',
      name: `${iface}-下行`,
      showSymbol: false,
      data: seriesDown,
      lineStyle: { width: 2, color: colors[colorIdx % colors.length] },
      smooth: false
    });

    series.push({
      type: 'line',
      name: `${iface}-上行`,
      showSymbol: false,
      data: seriesUp,
      lineStyle: { width: 2, color: colors[(colorIdx + 1) % colors.length] },
      smooth: false
    });
  });

  if (series.length > 0) {
    chartOptions[chartKey] = getMultiSeriesOption('各网卡 IO', series, true, true);
    if (!chartStates[chartKey]) {
      chartStates[chartKey] = { range: globalTimeRange.value };
    }
  }
};

// 加载存储IO历史数据
const loadStorageIoHistory = async (dev: string) => {
  const chartKey = `storage_io_${dev}`;
  const range = chartStates[chartKey]?.range || globalTimeRange.value;

  const data = await getHistory('storage_total_io', range);

  const seriesRead = data
    .filter(item => item.label === `${dev}-read`)
    .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

  const seriesWrite = data
    .filter(item => item.label === `${dev}-write`)
    .map(item => [item.timestamp, normalizeToBytes(item.value, item.unit)] as [number, number]);

  const option = chartOptions[chartKey];
  if (option && option.series) {
    (option.series as any)[0].data = seriesRead;
    (option.series as any)[1].data = seriesWrite;
  }
};

// 加载存储空间历史数据
const loadStorageSpaceHistory = async (dev: string) => {
  const chartKey = `storage_space_${dev}`;
  const range = chartStates[chartKey]?.range || globalTimeRange.value;

  const data = await getHistory('storage_space', range);

  const seriesData = data
    .filter(item => item.label === dev)
    .map(item => [item.timestamp, item.value] as [number, number]);

  const option = chartOptions[chartKey];
  if (option && (option.series as any)[0]) {
    (option.series as any)[0].data = seriesData;
  }
};

// ================= 动态初始化图表 =================

// 初始化 CPU 核心图表
const initCpuCoreCharts = (data: DynamicApiResponse) => {
  if (data.cpu) {
    Object.keys(data.cpu).forEach(key => {
      if (key === 'total') return;
      const chartKey = `cpu_core_${key}`;
      if (!chartOptions[chartKey]) {
        chartOptions[chartKey] = getFixedAxisOption(`${key}`, '#3b82f6', '%', 0, 100);
        if (!chartStates[chartKey]) {
          chartStates[chartKey] = { range: globalTimeRange.value };
        }
      }
    });
  }
};


// 初始化网卡 IO 图表
const initNetworkIfaceCharts = (data: DynamicApiResponse) => {
  if (data.network) {
    const chartKey = 'network_iface_all';
    if (!chartOptions[chartKey]) {
      chartOptions[chartKey] = getMultiSeriesOption('各网卡 IO', [], true, true);
      if (!chartStates[chartKey]) {
        chartStates[chartKey] = { range: globalTimeRange.value };
      }
    }
  }
};

// 初始化存储图表
const initStorageCharts = (data: DynamicApiResponse) => {
  if (data.storage) {
    Object.keys(data.storage).forEach(key => {
      if (key === 'total') return;

      const chartKeyIO = `storage_io_${key}`;
      if (!chartOptions[chartKeyIO]) {
        const series: any[] = [
          { type: 'line', name: `${key}-读`, showSymbol: false, data: [], lineStyle: { width: 2, color: '#10b981' }, smooth: false },
          { type: 'line', name: `${key}-写`, showSymbol: false, data: [], lineStyle: { width: 2, color: '#f97316' }, smooth: false }
        ];
        chartOptions[chartKeyIO] = getMultiSeriesOption(`${key} IO`, series, true, true);
        if (!chartStates[chartKeyIO]) {
          chartStates[chartKeyIO] = { range: globalTimeRange.value };
        }
      }

      const chartKeySpace = `storage_space_${key}`;
      if (!chartOptions[chartKeySpace]) {
        const storageData = data.storage[key];
        const maxValue = storageData.total.value;
        chartOptions[chartKeySpace] = getFixedAxisOption(`${key} 存储空间`, '#06b6d4', storageData.total.unit, 0, maxValue);
        if (!chartStates[chartKeySpace]) {
          chartStates[chartKeySpace] = { range: globalTimeRange.value };
        }
      }
    });
  }
};

// ================= 数据追加 =================

const appendDataPoint = (key: string, timestamp: number, value: number, unit: string) => {
  if (!chartOptions[key]) return;

  const seriesArr = (chartOptions[key].series as { data: [number, number][] })[0].data;
  let finalValue = value;
  const isIO = ['network_in', 'network_out', 'storage_io', 'storage_total_io'].includes(key);
  if (isIO) {
    finalValue = normalizeToBytes(value, unit);
    unit = 'B/S';
  }
  seriesArr.push([timestamp, finalValue]);

  if (seriesArr.length > 500) {
    seriesArr.shift();
  }

  const range = chartStates[key]?.range || globalTimeRange.value;
  const filteredData = filterDataByTimeRange(seriesArr, range);
  (chartOptions[key].series as any)[0].data = filteredData;
};

const appendDataPointMultiSeries = (chartKey: string, seriesIndex: number, timestamp: number, value: number) => {
  if (!chartOptions[chartKey]) return;

  const seriesArr = (chartOptions[chartKey].series as { data: [number, number][] })[seriesIndex].data;
  seriesArr.push([timestamp, value]);

  if (seriesArr.length > 500) {
    seriesArr.shift();
  }

  const range = chartStates[chartKey]?.range || globalTimeRange.value;
  const filteredData = filterDataByTimeRange(seriesArr, range);
  (chartOptions[chartKey].series as any)[seriesIndex].data = filteredData;
};

// ================= 监听数据流 =================

watch(() => props.data.dynamic, (newData) => {
  if (!newData || Object.keys(newData).length === 0) return;
  const now = Date.now();

  initCpuCoreCharts(newData);
  initNetworkIfaceCharts(newData);
  initStorageCharts(newData);
  initMemoryUsedChart(newData);

  // 基本指标 - CPU Total
  const cpuUsage = newData.cpu?.total?.usage;
  if (cpuUsage?.value !== undefined) appendDataPoint('cpu_total', now, cpuUsage.value, cpuUsage.unit);

  // 基本指标 - CPU Temp
  if (newData.cpu) {
    let totalTemp = 0, count = 0;
    Object.values(newData.cpu).forEach((c: any) => { if (c.temperature.value > 0) { totalTemp += c.temperature.value; count++ } });
    if (count > 0) {
      const unit = Object.values(newData.cpu)[0].temperature.unit;
      appendDataPoint('cpu_temp', now, totalTemp / count, unit);
    }
  }

  // 基本指标 - Memory Percent
  const memUsage = newData.memory?.used_percent;
  if (memUsage?.value !== undefined) appendDataPoint('memory_used_percent', now, memUsage.value, memUsage.unit);

  // 基本指标 - Connections
  if (props.data.connection?.counts) {
    const counts = props.data.connection.counts;
    const connOption = chartOptions.connections;
    if (connOption) {
      appendDataPointMultiSeries('connections', 0, now, counts.tcp);
      appendDataPointMultiSeries('connections', 1, now, counts.udp);
      appendDataPointMultiSeries('connections', 2, now, counts.other);
      appendDataPointMultiSeries('connections', 3, now, counts.tcp + counts.udp + counts.other);
    }
  }

  // CPU 核心
  if (newData.cpu) {
    Object.keys(newData.cpu).forEach(key => {
      if (key === 'total') return;
      const chartKey = `cpu_core_${key}`;
      const core = newData.cpu[key];
      if (core?.usage?.value !== undefined) {
        appendDataPoint(chartKey, now, core.usage.value, core.usage.unit);
      }
    });
  }

  // 内存使用量
  const memUsed = newData.memory?.used;
  if (memUsed?.value !== undefined && chartOptions.memory_used) {
    const memMax = newData.memory?.total?.value;
    if (memMax && chartOptions.memory_used.yAxis) {
      (chartOptions.memory_used.yAxis as any).max = memMax;
    }
    const [value, unit] = covertDataBytes(memUsed.value, memUsed.unit, newData.memory?.total?.unit);
    appendDataPoint('memory_used', now, value, unit);
  }

  // 网络 - 总网卡 IO
  if (newData.network?.total) {
    const netIn = newData.network.total.incoming;
    const netOut = newData.network.total.outgoing;
    if (netIn?.value !== undefined) {
      const value = normalizeToBytes(netIn.value, netIn.unit);
      appendDataPointMultiSeries('network_total', 0, now, value);
    }
    if (netOut?.value !== undefined) {
      const value = normalizeToBytes(netOut.value, netOut.unit);
      appendDataPointMultiSeries('network_total', 1, now, value);
    }
  }

  // 网络 - pppoe-wan IO
  if (newData.network?.['pppoe-wan']) {
    const netIn = newData.network['pppoe-wan'].incoming;
    const netOut = newData.network['pppoe-wan'].outgoing;
    if (netIn?.value !== undefined) {
      const value = normalizeToBytes(netIn.value, netIn.unit);
      appendDataPointMultiSeries('network_pppoe_wan', 0, now, value);
    }
    if (netOut?.value !== undefined) {
      const value = normalizeToBytes(netOut.value, netOut.unit);
      appendDataPointMultiSeries('network_pppoe_wan', 1, now, value);
    }
  }

  // 网络 - 各网卡 IO
  if (newData.network) {
    const interfaces = Object.keys(newData.network).filter(k => k !== 'total');
    const chartKey = 'network_iface_all';
    if (chartOptions[chartKey]) {
      const series = (chartOptions[chartKey].series as any[]);
      interfaces.forEach((iface, idx) => {
        const net = newData.network[iface];
        if (net?.incoming?.value !== undefined) {
          const value = normalizeToBytes(net.incoming.value, net.incoming.unit);
          if (series[idx * 2]) {
            const seriesArr = series[idx * 2].data as [number, number][];
            seriesArr.push([now, value]);
            if (seriesArr.length > 500) seriesArr.shift();
            const range = chartStates[chartKey]?.range || globalTimeRange.value;
            series[idx * 2].data = filterDataByTimeRange(seriesArr, range);
          }
        }
        if (net?.outgoing?.value !== undefined) {
          const value = normalizeToBytes(net.outgoing.value, net.outgoing.unit);
          if (series[idx * 2 + 1]) {
            const seriesArr = series[idx * 2 + 1].data as [number, number][];
            seriesArr.push([now, value]);
            if (seriesArr.length > 500) seriesArr.shift();
            const range = chartStates[chartKey]?.range || globalTimeRange.value;
            series[idx * 2 + 1].data = filterDataByTimeRange(seriesArr, range);
          }
        }
      });
    }
  }

  // 存储 - 总 IO
  if (newData.storage) {
    let totalBytes = 0;
    Object.values(newData.storage).forEach((d: StorageData) => {
      const readBytes = normalizeToBytes(d.read.value, d.read.unit);
      const writeBytes = normalizeToBytes(d.write.value, d.write.unit);
      if (readBytes > 0) totalBytes += readBytes;
      if (writeBytes > 0) totalBytes += writeBytes;
    });
    appendDataPoint('storage_total_io', now, totalBytes, 'B/S');
  }

  // 存储 - 各磁盘 IO 和空间
  if (newData.storage) {
    Object.keys(newData.storage).forEach(key => {
      if (key === 'total') return;
      const d = newData.storage[key];

      // IO
      const chartKeyIO = `storage_io_${key}`;
      const readBytes = normalizeToBytes(d.read.value, d.read.unit);
      const writeBytes = normalizeToBytes(d.write.value, d.write.unit);
      appendDataPointMultiSeries(chartKeyIO, 0, now, readBytes);
      appendDataPointMultiSeries(chartKeyIO, 1, now, writeBytes);

      // 空间
      const chartKeySpace = `storage_space_${key}`;
      appendDataPoint(chartKeySpace, now, d.used.value, d.used.unit);
    });
  }

}, { deep: true });

// ================= UI 交互 =================

const handleRangeChange = async (key: string) => {
  if (key === 'network_total') await loadNetworkTotalHistory();
  else if (key === 'network_pppoe_wan') await loadNetworkPppoeWanHistory();
  else if (key === 'connections') await loadConnectionsHistory();
  else if (key === 'network_iface_all') await loadNetworkIfaceHistory();
  else if (key.startsWith('storage_io_')) {
    const storageKey = key.replace('storage_io_', '');
    await loadStorageIoHistory(storageKey);
  }
  else if (key.startsWith('storage_space_')) {
    const storageKey = key.replace('storage_space_', '');
    await loadStorageSpaceHistory(storageKey);
  }
  else {
    await loadHistoryAndRender(key);
  }
};

const handleGlobalRangeChange = async (event: Event) => {
  const target = event.target as HTMLSelectElement;
  const newRange = Number(target.value);
  await setConfig('chart_time_range', newRange);

  Object.keys(chartStates).forEach(key => {
    chartStates[key].range = newRange;
  });

  await loadConnectionsHistory();
  await loadNetworkTotalHistory();
  await loadNetworkPppoeWanHistory();
  await loadNetworkIfaceHistory();

  Object.keys(chartOptions).forEach(key => {
    if (key.startsWith('storage_io_')) {
      const storageKey = key.replace('storage_io_', '');
      loadStorageIoHistory(storageKey);
    } else if (key.startsWith('storage_space_')) {
      const storageKey = key.replace('storage_space_', '');
      loadStorageSpaceHistory(storageKey);
    } else if (!['connections', 'network_total', 'network_pppoe_wan', 'network_iface_all'].includes(key)) {
      loadHistoryAndRender(key);
    }
  });
};

onMounted(async () => {
  await nextTick();

  // 加载基本指标
  await loadHistoryAndRender('cpu_total');
  await loadHistoryAndRender('cpu_temp');
  await loadHistoryAndRender('memory_used_percent');
  await loadConnectionsHistory();

  // 加载网络数据
  await loadNetworkTotalHistory();
  await loadNetworkPppoeWanHistory();
  await loadNetworkIfaceHistory();

  // 加载存储数据
  await loadHistoryAndRender('storage_total_io');

  // 加载CPU核心数据
  Object.keys(chartOptions).filter(k => k.startsWith('cpu_core_')).forEach(k => loadHistoryAndRender(k));

  // 加载内存使用量
  if (chartOptions.memory_used) {
    await loadHistoryAndRender('memory_used');
  }

  // 加载存储IO和空间
  Object.keys(chartOptions).filter(k => k.startsWith('storage_io_')).forEach(k => {
    const storageKey = k.replace('storage_io_', '');
    loadStorageIoHistory(storageKey);
  });
  Object.keys(chartOptions).filter(k => k.startsWith('storage_space_')).forEach(k => {
    const storageKey = k.replace('storage_space_', '');
    loadStorageSpaceHistory(storageKey);
  });
});
</script>

<template>
  <div class="w-full h-full flex flex-col gap-6">
    <div class="py-2.5 border-b border-slate-700 mb-5 flex justify-between items-center">
      <div class="flex items-center gap-4">
        <h3 class="text-lg font-semibold text-slate-200">监控图表</h3>
      </div>

      <div class="flex items-center gap-2">
        <div class="text-slate-400 text-sm text-right">全局图表时间范围 :</div>
        <div class="relative">
          <select :value="settings.chart_time_range" @change="handleGlobalRangeChange"
            class="bg-slate-900 border border-slate-600 text-white text-xs px-2 py-1 rounded outline-none focus:border-blue-500">
            <option v-for="r in TimeRanges" :key="r.value" :value="r.value">{{ r.label }}</option>
          </select>
        </div>
      </div>
    </div>

    <!-- 基本指标分类 -->
    <div>
      <div @click="toggleAccordion('basic')"
        class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
        <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">基本指标</h3>
        <span class="text-slate-500 transition-transform duration-300"
          :class="{ 'rotate-180': uiState.accordions.basic }">▼</span>
      </div>
      <div v-show="uiState.accordions.basic" class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div v-for="[key, opt] in getBasicCharts" :key="key"
          class="bg-slate-800 border border-slate-700 rounded-xl p-4 relative group">
          <select :value="chartStates[key]?.range || globalTimeRange"
            @change="(e) => { chartStates[key] = { range: Number((e.target as HTMLSelectElement).value) }; handleRangeChange(key); }"
            class="absolute top-6 right-16 z-10 bg-slate-900 border border-slate-600 text-xs text-slate-300 px-2 py-1 rounded outline-none opacity-100 transition-opacity">
            <option v-for="r in TimeRanges" :key="r.value" :value="r.value">{{ r.label }}</option>
          </select>
          <v-chart :option="opt" :autoresize="true" style="height: 320px;" />
        </div>
      </div>
    </div>

    <!-- CPU 分类 -->
    <div>
      <div @click="toggleAccordion('cpu')"
        class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
        <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">CPU</h3>
        <span class="text-slate-500 transition-transform duration-300"
          :class="{ 'rotate-180': uiState.accordions.cpu }">▼</span>
      </div>
      <div v-show="uiState.accordions.cpu" class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div v-for="[key, opt] in getCpuCoreCharts" :key="key"
          class="bg-slate-800 border border-slate-700 rounded-xl p-4 relative group">
          <select :value="chartStates[key]?.range || globalTimeRange"
            @change="(e) => { chartStates[key] = { range: Number((e.target as HTMLSelectElement).value) }; handleRangeChange(key); }"
            class="absolute top-6 right-16 z-10 bg-slate-900 border border-slate-600 text-xs text-slate-300 px-2 py-1 rounded outline-none opacity-100 transition-opacity">
            <option v-for="r in TimeRanges" :key="r.value" :value="r.value">{{ r.label }}</option>
          </select>
          <v-chart :option="opt" :autoresize="true" style="height: 320px;" />
        </div>
      </div>
    </div>

    <!-- 内存分类 -->
    <div>
      <div @click="toggleAccordion('memory')"
        class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
        <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">内存</h3>
        <span class="text-slate-500 transition-transform duration-300"
          :class="{ 'rotate-180': uiState.accordions.memory }">▼</span>
      </div>
      <div v-show="uiState.accordions.memory" class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div v-for="[key, opt] in getMemoryCharts" :key="key"
          class="bg-slate-800 border border-slate-700 rounded-xl p-4 relative group">
          <select :value="chartStates[key]?.range || globalTimeRange"
            @change="(e) => { chartStates[key] = { range: Number((e.target as HTMLSelectElement).value) }; handleRangeChange(key); }"
            class="absolute top-6 right-16 z-10 bg-slate-900 border border-slate-600 text-xs text-slate-300 px-2 py-1 rounded outline-none opacity-100 transition-opacity">
            <option v-for="r in TimeRanges" :key="r.value" :value="r.value">{{ r.label }}</option>
          </select>
          <v-chart :option="opt" :autoresize="true" style="height: 320px;" />
        </div>
      </div>
    </div>

    <!-- 网络分类 -->
    <div>
      <div @click="toggleAccordion('network')"
        class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
        <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">网络</h3>
        <span class="text-slate-500 transition-transform duration-300"
          :class="{ 'rotate-180': uiState.accordions.network }">▼</span>
      </div>
      <div v-show="uiState.accordions.network" class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div v-for="[key, opt] in getNetworkCharts" :key="key"
          class="bg-slate-800 border border-slate-700 rounded-xl p-4 relative group lg:col-span-2">
          <select :value="chartStates[key]?.range || globalTimeRange"
            @change="(e) => { chartStates[key] = { range: Number((e.target as HTMLSelectElement).value) }; handleRangeChange(key); }"
            class="absolute top-6 right-16 z-10 bg-slate-900 border border-slate-600 text-xs text-slate-300 px-2 py-1 rounded outline-none opacity-100 transition-opacity">
            <option v-for="r in TimeRanges" :key="r.value" :value="r.value">{{ r.label }}</option>
          </select>
          <v-chart :option="opt" :autoresize="true" style="height: 320px;" />
        </div>
      </div>
    </div>

    <!-- 存储分类 -->
    <div>
      <div @click="toggleAccordion('storage')"
        class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
        <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">存储</h3>
        <span class="text-slate-500 transition-transform duration-300"
          :class="{ 'rotate-180': uiState.accordions.storage }">▼</span>
      </div>
      <div v-show="uiState.accordions.storage" class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div v-for="[key, opt] in getStorageCharts" :key="key"
          class="bg-slate-800 border border-slate-700 rounded-xl p-4 relative group">
          <select :value="chartStates[key]?.range || globalTimeRange"
            @change="(e) => { chartStates[key] = { range: Number((e.target as HTMLSelectElement).value) }; handleRangeChange(key); }"
            class="absolute top-6 right-16 z-10 bg-slate-900 border border-slate-600 text-xs text-slate-300 px-2 py-1 rounded outline-none opacity-100 transition-opacity">
            <option v-for="r in TimeRanges" :key="r.value" :value="r.value">{{ r.label }}</option>
          </select>
          <v-chart :option="opt" :autoresize="true" style="height: 320px;" />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
div[ref] {
  width: 100%;
  height: 100%;
}
</style>
