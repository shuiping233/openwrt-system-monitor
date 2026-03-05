<script setup lang="ts">
import { ref, computed, h, watch, reactive, onMounted } from 'vue';
import {
  useVueTable,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  FlexRender,
  createColumnHelper,
  SortingState,
  ColumnFiltersState
} from '@tanstack/vue-table';
import type { ConnectionApiResponse, AggregationTrafficResponse, AggregationTrafficDetails, IpAddressType } from '../model';
import { compressIPv6 } from '../utils/ipv6';
import { convertToBytes, BytesFixed, formatIOBytes, normalizeToBytes } from '../utils/convert';
import { useToast } from '../useToast';
import { useDatabase } from '../useDatabase';

// Props
const props = defineProps<{
  connectionData?: ConnectionApiResponse;
  aggregationData?: AggregationTrafficResponse;
}>();

// Database
const { getAccordionState, setAccordionState } = useDatabase();

// ================= 1. 折叠状态管理 =================
const uiState = reactive({
  accordions: {
    aggregation: true,  // 聚合统计折叠状态
    connectionList: true,  // 连接列表折叠状态
  },
  ipGroupCollapsed: {
    lan: false,  // 局域网IP组折叠状态
    wan: false,  // 外网IP组折叠状态
    unknown: false,  // 未知IP组折叠状态
  }
});

// 加载折叠状态
onMounted(async () => {
  // 主折叠栏状态
  const aggregationState = await getAccordionState('network_connection_aggregation');
  if (aggregationState !== undefined) {
    uiState.accordions.aggregation = aggregationState;
  }

  const connectionListState = await getAccordionState('network_connection_list');
  if (connectionListState !== undefined) {
    uiState.accordions.connectionList = connectionListState;
  }

  // IP分组折叠状态
  const lanGroupState = await getAccordionState('network_connection_lan_group');
  if (lanGroupState !== undefined) {
    uiState.ipGroupCollapsed.lan = !lanGroupState; // 注意：存储的是展开状态，我们用的是折叠状态
  }

  const wanGroupState = await getAccordionState('network_connection_wan_group');
  if (wanGroupState !== undefined) {
    uiState.ipGroupCollapsed.wan = !wanGroupState;
  }

  const unknownGroupState = await getAccordionState('network_connection_unknown_group');
  if (unknownGroupState !== undefined) {
    uiState.ipGroupCollapsed.unknown = !unknownGroupState;
  }
});

// 切换主折叠状态
const toggleMainAccordion = async (key: 'aggregation' | 'connectionList') => {
  uiState.accordions[key] = !uiState.accordions[key];
  const storageKey = key === 'aggregation' ? 'network_connection_aggregation' : 'network_connection_list';
  await setAccordionState(storageKey, uiState.accordions[key]);
};

// 切换IP分组折叠状态
const toggleIpGroup = async (group: IpAddressType) => {
  uiState.ipGroupCollapsed[group] = !uiState.ipGroupCollapsed[group];
  const storageKeyMap: Record<IpAddressType, string> = {
    lan: 'network_connection_lan_group',
    wan: 'network_connection_wan_group',
    unknown: 'network_connection_unknown_group'
  };
  // 存储展开状态（与折叠状态相反）
  await setAccordionState(storageKeyMap[group], !uiState.ipGroupCollapsed[group]);
};

// ================= 2. 全局搜索词 =================
const globalFilter = ref('');
const aggregationFilter = ref(''); // 聚合统计的搜索词

watch(globalFilter, (newFilter) => {
  table.setGlobalFilter(newFilter);
});

// ================= 3. 聚合统计排序状态 =================
type SortDirection = 'asc' | 'desc' | null;
type SortColumn = 'ip' | 'traffic' | 'upload' | 'download' | 'tcp' | 'udp' | 'other';

const aggregationSort = reactive<{
  column: SortColumn;
  direction: SortDirection;
}>({
  column: 'traffic',
  direction: 'desc'
});

const toggleAggregationSort = (column: SortColumn) => {
  if (aggregationSort.column === column) {
    // 切换方向
    if (aggregationSort.direction === 'desc') {
      aggregationSort.direction = 'asc';
    } else if (aggregationSort.direction === 'asc') {
      aggregationSort.direction = null;
    } else {
      aggregationSort.direction = 'desc';
    }
  } else {
    // 新列，默认降序
    aggregationSort.column = column;
    aggregationSort.direction = 'desc';
  }
};

const getSortIcon = (column: SortColumn): string => {
  if (aggregationSort.column !== column) return '';
  if (aggregationSort.direction === 'asc') return '↑';
  if (aggregationSort.direction === 'desc') return '↓';
  return '';
};

// ================= 4. 处理连接数据（去重） =================
const displayData = computed(() => {
  const list = props.connectionData?.connections || [];
  if (list.length === 0) return [];

  // 使用 Set 来跟踪已见过的连接标识符，防止重复
  const seen = new Set();
  return list.filter(connection => {
    const endpointA = `${connection.source_ip}:${connection.source_port}`;
    const endpointB = `${connection.destination_ip}:${connection.destination_port}`;
    // 对端点进行排序以处理双向连接
    const endpoints = [endpointA, endpointB].sort();
    const key = `${endpoints[0]}<->${endpoints[1]}-${connection.protocol}`;

    if (seen.has(key)) {
      return false; // 过滤掉重复项
    }
    seen.add(key);
    return true;
  });
});

// ================= 6. 聚合统计数据计算 =================
interface TrafficMetric {
  value: number;
  unit: string;
  bytes: number; // 转换为字节用于排序
}

interface IPStats {
  ip: string;
  ipType: IpAddressType;
  traffic: TrafficMetric;  // 总流量 - total_throughput
  upload: TrafficMetric;   // 上行流量 - incoming
  download: TrafficMetric; // 下行流量 - outgoing
  tcpCount: number;
  udpCount: number;
  otherCount: number;
}

interface GroupStats {
  name: string;
  key: IpAddressType;
  ips: IPStats[];
  totalTraffic: number; // 字节数
  totalUpload: number;  // 字节数
  totalDownload: number; // 字节数
  totalTcp: number;
  totalUdp: number;
  totalOther: number;
}

// 辅助函数：将MetricUnit转换为字节
const metricUnitToBytes = (metric: { value: number; unit: string }): number => {
  return normalizeToBytes(metric.value, metric.unit);
};

// 排序函数
const sortIPStats = (ips: IPStats[], column: SortColumn, direction: SortDirection): IPStats[] => {
  if (!direction) return ips;

  const sorted = [...ips];
  const multiplier = direction === 'desc' ? -1 : 1;

  sorted.sort((a, b) => {
    let comparison = 0;
    switch (column) {
      case 'ip':
        comparison = a.ip.localeCompare(b.ip);
        break;
      case 'traffic':
        comparison = a.traffic.bytes - b.traffic.bytes;
        break;
      case 'upload':
        comparison = a.upload.bytes - b.upload.bytes;
        break;
      case 'download':
        comparison = a.download.bytes - b.download.bytes;
        break;
      case 'tcp':
        comparison = a.tcpCount - b.tcpCount;
        break;
      case 'udp':
        comparison = a.udpCount - b.udpCount;
        break;
      case 'other':
        comparison = a.otherCount - b.otherCount;
        break;
    }
    return comparison * multiplier;
  });

  return sorted;
};

// 过滤函数
const filterIPStats = (ips: IPStats[], filter: string): IPStats[] => {
  if (!filter.trim()) return ips;
  const lowerFilter = filter.toLowerCase();
  return ips.filter(ip => {
    return ip.ip.toLowerCase().includes(lowerFilter) ||
      String(ip.traffic.value).includes(lowerFilter) ||
      ip.traffic.unit.toLowerCase().includes(lowerFilter) ||
      String(ip.upload.value).includes(lowerFilter) ||
      ip.upload.unit.toLowerCase().includes(lowerFilter) ||
      String(ip.download.value).includes(lowerFilter) ||
      ip.download.unit.toLowerCase().includes(lowerFilter) ||
      String(ip.tcpCount).includes(lowerFilter) ||
      String(ip.udpCount).includes(lowerFilter) ||
      String(ip.otherCount).includes(lowerFilter);
  });
};

const aggregationData = computed((): { lan: GroupStats; wan: GroupStats; unknown: GroupStats } => {
  // 从API数据中提取所有details
  let allDetails: AggregationTrafficDetails[] = [];

  if (props.aggregationData?.details) {
    allDetails = props.aggregationData.details;
  }

  // 转换为IPStats结构
  const ipStatsList: IPStats[] = allDetails.map((detail) => ({
    ip: detail.ip,
    ipType: detail.ip_type,
    traffic: {
      value: detail.total_throughput.value,
      unit: detail.total_throughput.unit,
      bytes: metricUnitToBytes(detail.total_throughput),
    },
    upload: {
      value: detail.outgoing.value,
      unit: detail.outgoing.unit,
      bytes: metricUnitToBytes(detail.outgoing),
    },
    download: {
      value: detail.incoming.value,
      unit: detail.incoming.unit,
      bytes: metricUnitToBytes(detail.incoming),
    },
    tcpCount: detail.tcp,
    udpCount: detail.udp,
    otherCount: detail.other,
  }));

  // 按ip_type分组
  let lanIPs: IPStats[] = ipStatsList.filter((ip) => ip.ipType === 'lan');
  let wanIPs: IPStats[] = ipStatsList.filter((ip) => ip.ipType === 'wan');
  let unknownIPs: IPStats[] = ipStatsList.filter((ip) => ip.ipType === 'unknown');

  // 应用搜索过滤
  lanIPs = filterIPStats(lanIPs, aggregationFilter.value);
  wanIPs = filterIPStats(wanIPs, aggregationFilter.value);
  unknownIPs = filterIPStats(unknownIPs, aggregationFilter.value);

  // 应用排序
  lanIPs = sortIPStats(lanIPs, aggregationSort.column, aggregationSort.direction);
  wanIPs = sortIPStats(wanIPs, aggregationSort.column, aggregationSort.direction);
  unknownIPs = sortIPStats(unknownIPs, aggregationSort.column, aggregationSort.direction);

  // 计算分组汇总（基于过滤后的数据）
  const calculateGroupTotal = (ips: IPStats[], key: IpAddressType, name: string): GroupStats => {
    return {
      name,
      key,
      ips,
      totalTraffic: ips.reduce((sum, ip) => sum + ip.traffic.bytes, 0),
      totalUpload: ips.reduce((sum, ip) => sum + ip.upload.bytes, 0),
      totalDownload: ips.reduce((sum, ip) => sum + ip.download.bytes, 0),
      totalTcp: ips.reduce((sum, ip) => sum + (ip.tcpCount >= 0 ? ip.tcpCount : 0), -1),
      totalUdp: ips.reduce((sum, ip) => sum + (ip.udpCount >= 0 ? ip.udpCount : 0), -1),
      totalOther: ips.reduce((sum, ip) => sum + (ip.otherCount >= 0 ? ip.otherCount : 0), -1),
    };
  };

  return {
    lan: calculateGroupTotal(lanIPs, 'lan', '局域网IP'),
    wan: calculateGroupTotal(wanIPs, 'wan', '外网IP'),
    unknown: calculateGroupTotal(unknownIPs, 'unknown', '未知IP'),
  };
});

// ================= 7. 辅助函数 =================
const formatIP = (ip: string | undefined, family: string | undefined): string => {
  if (!ip) return '-';
  if (family?.toUpperCase() === 'IPV6') {
    return `[${compressIPv6(ip)}]`;
  }
  return ip;
};

// 格式化流量显示
const formatTraffic = (bytes: number): string => {
  if (bytes === 0) return '0 B';
  return formatIOBytes(bytes);
};

// 复制功能
const copyInfo = (row: any) => {
  let source_ip: string = row.source_ip
  let destination_ip: string = row.destination_ip

  if (row.ip_family?.toUpperCase() === 'IPV6') {
    source_ip = `[${compressIPv6(row.source_ip)}]`;
    destination_ip = `[${compressIPv6(row.destination_ip)}]`;
  }

  const text = `[${row.ip_family}] ${row.protocol} ${source_ip}:${row.source_port} -> ${destination_ip}:${row.destination_port} | 状态: ${row.state || '-'} | 流量: ${row.traffic.value.toFixed(2)} ${row.traffic.unit} (${row.packets} Pkgs)`;

  // 检查浏览器是否支持 Clipboard API
  if (navigator.clipboard && window.isSecureContext) {
    // 现代浏览器的安全上下文
    navigator.clipboard.writeText(text).then(() => {
      const { success } = useToast();
      success('连接信息已复制！');
    }).catch((err) => {
      console.error('复制失败:', err);
      // 降级到传统方法
      fallbackCopyTextToClipboard(text);
    });
  } else {
    // 降级到传统方法
    fallbackCopyTextToClipboard(text);
  }
};

// 传统复制方法（兼容不支持 Clipboard API 的浏览器）
const fallbackCopyTextToClipboard = (text: string) => {
  const textArea = document.createElement('textarea');
  textArea.value = text;

  // 避免滚动到底部
  textArea.style.top = '0';
  textArea.style.left = '0';
  textArea.style.position = 'fixed';
  textArea.style.opacity = '0';

  document.body.appendChild(textArea);
  textArea.focus();
  textArea.select();

  try {
    const successful = document.execCommand('copy');
    if (successful) {
      const { success } = useToast();
      success('连接信息已复制！');
    } else {
      const { error } = useToast();
      error('复制失败，请手动复制');
    }
  } catch (err) {
    console.error('传统复制方法失败:', err);
    const { error } = useToast();
    error('复制失败，请手动复制');
  }

  document.body.removeChild(textArea);
};

// ================= 8. TanStack Table 配置 (使用 h 函数代替 JSX 以避免 TS 解析错误) =================
const columnHelper = createColumnHelper<any>();

// 只允许同时排列一行的排序函数
const createSingleSortFn = (columnId: string) => {
  return (rowA: any, rowB: any) => {
    const valA = rowA.original[columnId] || '';
    const valB = rowB.original[columnId] || '';
    return String(valA).localeCompare(String(valB));
  };
};

const columns = [
  // 地址族
  columnHelper.accessor('ip_family', {
    header: '地址族',
    cell: (info) => h('span', { class: 'bg-slate-700 px-2 py-1 rounded text-xs text-slate-200' }, info.getValue()?.toUpperCase()),
    enableSorting: true,
    sortingFn: createSingleSortFn('ip_family'),
  }),
  // 协议
  columnHelper.accessor('protocol', {
    header: '协议',
    cell: (info) => h('span', { class: 'bg-slate-700 px-2 py-1 rounded text-xs text-slate-200' }, info.getValue()?.toUpperCase()),
    enableSorting: true,
    sortingFn: createSingleSortFn('protocol'),
  }),
  // 源地址
  columnHelper.accessor('source_ip', {
    header: '源地址',
    cell: (info) => {
      const row = info.row.original;
      const ip = info.getValue();
      const port = row.source_port;
      return h('span', { class: 'font-mono text-slate-300' }, formatIP(ip, row.ip_family) + (port > 0 ? ':' + port : ''));
    },
    enableSorting: true,
    sortingFn: (rowA, rowB) => {
      const ipA = formatIP(rowA.original.source_ip, rowA.original.ip_family);
      const portA = rowA.original.source_port;
      const ipB = formatIP(rowB.original.source_ip, rowB.original.ip_family);
      const portB = rowB.original.source_port;

      // 首先按IP地址排序
      const ipComparison = ipA.localeCompare(ipB);
      if (ipComparison !== 0) {
        return ipComparison;
      }
      // IP相同时按端口号数值排序
      return portA - portB;
    },
    filterFn: (row, columnId, filterValue) => {
      const ip = row.getValue(columnId);
      const port = row.original.source_port;
      const family = typeof row.original.ip_family === 'string' ? row.original.ip_family : '';
      const fullAddress = `${formatIP(ip as string, family)}:${port}`;
      return fullAddress.toLowerCase().includes(filterValue.toLowerCase());
    },
  }),
  // 目标地址
  columnHelper.accessor('destination_ip', {
    header: '目标地址',
    cell: (info) => {
      const row = info.row.original;
      const ip = info.getValue();
      const port = row.destination_port;
      return h('span', { class: 'font-mono text-slate-300' }, formatIP(ip, row.ip_family) + (port > 0 ? ':' + port : ''));
    },
    enableSorting: true,
    sortingFn: (rowA, rowB) => {
      const ipA = formatIP(rowA.original.destination_ip, rowA.original.ip_family);
      const portA = rowA.original.destination_port;
      const ipB = formatIP(rowB.original.destination_ip, rowB.original.ip_family);
      const portB = rowB.original.destination_port;

      // 首先按IP地址排序
      const ipComparison = ipA.localeCompare(ipB);
      if (ipComparison !== 0) {
        return ipComparison;
      }
      // IP相同时按端口号数值排序
      return portA - portB;
    },
    filterFn: (row, columnId, filterValue) => {
      const ip = row.getValue(columnId);
      const port = row.original.destination_port;
      const family = typeof row.original.ip_family === 'string' ? row.original.ip_family : '';
      const fullAddress = `${formatIP(ip as string, family)}:${port}`;
      return fullAddress.toLowerCase().includes(filterValue.toLowerCase());
    },
  }),
  // 状态
  columnHelper.accessor('state', {
    header: '状态',
    cell: (info) => h('span', { class: 'text-slate-300' }, info.getValue() || '-'),
    enableSorting: true,
    sortingFn: createSingleSortFn('state'),
  }),
  // 传输情况
  columnHelper.accessor('traffic', {
    header: '传输情况',
    cell: (info) => {
      const row = info.row.original;
      return h('span', { class: 'text-slate-300' }, BytesFixed(row.traffic.value, row.traffic.unit) + ' ' + row.traffic.unit + ' (' + row.packets + ' Pkgs.)');
    },
    sortingFn: (rowA, rowB) => {
      const valA = rowA.original.traffic.value || 0;
      const unitA = rowA.original.traffic.unit || 'B';
      const valB = rowB.original.traffic.value || 0;
      const unitB = rowB.original.traffic.unit || 'B';

      // 将不同单位转换为字节进行比较
      const bytesA = convertToBytes(valA, unitA);
      const bytesB = convertToBytes(valB, unitB);

      return bytesA - bytesB;
    },
    enableSorting: true,
    filterFn: (row, columnId, filterValue) => {
      const trafficValue = row.original.traffic.value || 0;
      const trafficUnit = row.original.traffic.unit || '';
      const packets = row.original.packets || 0;

      // 格式化后的值
      const formattedValue = BytesFixed(trafficValue, trafficUnit);
      const fullDisplayValue = `${formattedValue} ${trafficUnit} (${packets} Pkgs.)`;

      // 转换为小写进行比较
      const lowerFilterValue = filterValue.toLowerCase();
      const lowerDisplayValue = fullDisplayValue.toLowerCase();

      // 检查是否包含过滤值（支持数字和单位的搜索，忽略空格）
      if (lowerDisplayValue.replace(/\s+/g, '').includes(lowerFilterValue.replace(/\s+/g, ''))) {
        return true;
      }

      // 检查是否包含过滤值（支持数字和单位的搜索，保留空格）
      if (lowerDisplayValue.includes(lowerFilterValue)) {
        return true;
      }

      // 检查数值部分
      if (String(trafficValue).toLowerCase().includes(lowerFilterValue)) {
        return true;
      }

      // 检查单位部分
      if (trafficUnit.toLowerCase().includes(lowerFilterValue)) {
        return true;
      }

      // 检查包数
      if (String(packets).toLowerCase().includes(lowerFilterValue)) {
        return true;
      }

      // 检查不带空格的组合
      const noSpaceValue = `${formattedValue}${trafficUnit}(${packets}Pkgs.)`.toLowerCase();
      if (noSpaceValue.includes(lowerFilterValue.replace(/\s+/g, ''))) {
        return true;
      }

      return false;
    },
  }),
  // 操作列
  columnHelper.display({
    id: 'actions',
    header: '操作',
    cell: ({ row }) => h('button', {
      onClick: () => copyInfo(row.original),
      class: 'text-xs bg-slate-700 hover:bg-blue-600 text-white px-2 py-1 rounded transition-colors',
      title: '复制连接信息'
    }, '复制'),
    enableSorting: false,
  }),
];

// 初始状态 - 只允许同时排列一行
const initialSorting = [{ id: 'traffic', desc: true }] as SortingState;

const table = useVueTable({
  data: displayData,
  columns,
  getCoreRowModel: getCoreRowModel(),
  getSortedRowModel: getSortedRowModel(),
  getFilteredRowModel: getFilteredRowModel(),
  enableMultiSort: false, // 只允许同时排列一行
  getRowId: (row, index, parent) => {
    // 为每个连接创建一个标准化的唯一ID
    const endpointA = `${row.source_ip}:${row.source_port}`;
    const endpointB = `${row.destination_ip}:${row.destination_port}`;
    const endpoints = [endpointA, endpointB].sort(); // 排序确保一致性
    const baseId = `${endpoints[0]}<->${endpoints[1]}-${row.protocol}`;

    // 添加一个稳定的唯一标识符，基于连接信息和原始索引
    return `${baseId}-${row.traffic.value}-${row.packets}-${index}`;
  },
  initialState: {
    sorting: initialSorting,
    columnFilters: [],
    globalFilter: globalFilter.value,
  },
  globalFilterFn: (row, columnId, value) => {
    const search = String(value).toLowerCase();
    const rowStr = Object.values(row.original).join(' ').toLowerCase();
    return rowStr.includes(search);
  },
});

// 获取排序状态的显示
const getConnectionSortIcon = (columnId: string): string => {
  const sorting = table.getState().sorting;
  if (sorting.length === 0 || sorting[0].id !== columnId) return '';
  return sorting[0].desc ? '↓' : '↑';
};
</script>

<template>
  <div class="flex flex-col h-full space-y-6">
    <!-- Counts -->
    <div class="grid grid-cols-1 md:grid-cols-3 gap-5">
      <div
        class="bg-slate-800 border border-slate-700 rounded-xl p-5 border-t-4 border-t-blue-500 flex items-center justify-between">
        <div>
          <div class="text-slate-400 text-sm">TCP 连接</div>
          <div class="text-3xl font-bold">{{ connectionData?.counts?.tcp || 0 }}</div>
        </div>
        <div class="text-blue-500/20 text-4xl">T</div>
      </div>
      <div
        class="bg-slate-800 border border-slate-700 rounded-xl p-5 border-t-4 border-t-violet-500 flex items-center justify-between">
        <div>
          <div class="text-slate-400 text-sm">UDP 连接</div>
          <div class="text-3xl font-bold">{{ connectionData?.counts?.udp || 0 }}</div>
        </div>
        <div class="text-violet-500/20 text-4xl">U</div>
      </div>
      <div
        class="bg-slate-800 border border-slate-700 rounded-xl p-5 border-t-4 border-t-white flex items-center justify-between">
        <div>
          <div class="text-slate-400 text-sm">其他连接</div>
          <div class="text-3xl font-bold">{{ connectionData?.counts?.other || 0 }}</div>
        </div>
        <div class="text-white/20 text-4xl">?</div>
      </div>
    </div>

    <!-- 1. 聚合统计表格 -->
    <div>
      <!-- 聚合统计折叠栏 -->
      <div @click="toggleMainAccordion('aggregation')"
        class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
        <div class="flex items-center gap-4">
          <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">聚合统计</h3>
          <span class="text-xs text-slate-500">按 IP 地址聚合统计</span>
        </div>
        <span class="text-slate-500 transition-transform duration-300"
          :class="{ 'rotate-180': uiState.accordions.aggregation }">▼</span>
      </div>

      <!-- 聚合统计内容 -->
      <div v-show="uiState.accordions.aggregation"
        class="bg-slate-800 border border-slate-700 rounded-xl overflow-hidden">
        <!-- 全局搜索框（单独一行，居右）带清空按钮 -->
        <div class="px-4 py-3 border-b border-slate-700 flex justify-end">
          <div class="relative">
            <input v-model="aggregationFilter" placeholder="搜索 IP、流量、连接数..."
              class="bg-slate-900 border border-slate-600 text-white text-xs px-3 py-1.5 pr-8 rounded w-56 outline-none focus:border-blue-500" />
            <button v-if="aggregationFilter" @click="aggregationFilter = ''"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300 text-xs w-4 h-4 flex items-center justify-center rounded hover:bg-slate-700 transition-colors"
              title="清空搜索">
              ×
            </button>
          </div>
        </div>

        <div class="overflow-x-auto">
          <table class="w-full text-sm text-center border-collapse">
            <thead class="bg-slate-700/50 text-slate-300">
              <tr>
                <th @click="toggleAggregationSort('ip')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    IP 地址
                    <span class="text-slate-400">{{ getSortIcon('ip') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('traffic')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    实时流量
                    <span class="text-slate-400">{{ getSortIcon('traffic') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('upload')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    实时上行
                    <span class="text-slate-400">{{ getSortIcon('upload') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('download')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    实时下行
                    <span class="text-slate-400">{{ getSortIcon('download') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('tcp')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    TCP 连接
                    <span class="text-slate-400">{{ getSortIcon('tcp') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('udp')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    UDP 连接
                    <span class="text-slate-400">{{ getSortIcon('udp') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('other')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    其他连接
                    <span class="text-slate-400">{{ getSortIcon('other') }}</span>
                  </div>
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-700">
              <!-- 局域网IP分组 -->
              <tr class="bg-slate-700/30 hover:bg-slate-700/50 transition-colors cursor-pointer"
                @click="toggleIpGroup('lan')">
                <td colspan="7" class="px-3 py-3 text-left">
                  <div class="flex items-center justify-between">
                    <div class="flex items-center gap-2">
                      <span class="text-slate-500 transition-transform duration-300"
                        :class="{ 'rotate-180': !uiState.ipGroupCollapsed.lan }">▼</span>
                      <span class="font-semibold text-slate-200">{{ aggregationData.lan.name }}</span>
                      <span class="text-xs text-slate-500">({{ aggregationData.lan.ips.length }} 个 IP)</span>
                    </div>
                    <div class="flex items-center gap-4 text-xs">
                      <span class="text-slate-400">总流量: <span class="text-slate-200 font-mono">{{
                        formatTraffic(aggregationData.lan.totalTraffic) }}</span></span>
                      <span class="text-slate-400">上行: <span class="text-orange-400 font-mono">{{
                        formatTraffic(aggregationData.lan.totalUpload) }}</span></span>
                      <span class="text-slate-400">下行: <span class="text-cyan-400 font-mono">{{
                        formatTraffic(aggregationData.lan.totalDownload) }}</span></span>
                      <span class="text-slate-400">TCP: <span class="text-slate-200 font-mono">{{
                        aggregationData.lan.totalTcp }}</span></span>
                      <span class="text-slate-400">UDP: <span class="text-slate-200 font-mono">{{
                        aggregationData.lan.totalUdp }}</span></span>
                      <span class="text-slate-400">其他: <span class="text-slate-200 font-mono">{{
                        aggregationData.lan.totalOther }}</span></span>
                    </div>
                  </div>
                </td>
              </tr>
              <!-- 局域网IP详细行 -->
              <tr v-for="ipStats in aggregationData.lan.ips" :key="ipStats.ip" v-show="!uiState.ipGroupCollapsed.lan"
                class="hover:bg-slate-700/30 transition-colors">
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-300">{{ ipStats.ip }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.traffic.value.toFixed(2) }} {{ ipStats.traffic.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-orange-400">{{ ipStats.upload.value.toFixed(2) }} {{ ipStats.upload.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-cyan-400">{{ ipStats.download.value.toFixed(2) }} {{ ipStats.download.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.tcpCount }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.udpCount }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.otherCount }}</span>
                </td>
              </tr>
              <tr v-if="aggregationData.lan.ips.length === 0 && !uiState.ipGroupCollapsed.lan">
                <td colspan="7" class="px-5 py-4 text-center text-slate-500 text-xs">暂无局域网IP数据</td>
              </tr>

              <!-- 外网IP分组 -->
              <tr class="bg-slate-700/30 hover:bg-slate-700/50 transition-colors cursor-pointer"
                @click="toggleIpGroup('wan')">
                <td colspan="7" class="px-3 py-3 text-left">
                  <div class="flex items-center justify-between">
                    <div class="flex items-center gap-2">
                      <span class="text-slate-500 transition-transform duration-300"
                        :class="{ 'rotate-180': !uiState.ipGroupCollapsed.wan }">▼</span>
                      <span class="font-semibold text-slate-200">{{ aggregationData.wan.name }}</span>
                      <span class="text-xs text-slate-500">({{ aggregationData.wan.ips.length }} 个 IP)</span>
                    </div>
                    <div class="flex items-center gap-4 text-xs">
                      <span class="text-slate-400">总流量: <span class="text-slate-200 font-mono">{{
                        formatTraffic(aggregationData.wan.totalTraffic) }}</span></span>
                      <span class="text-slate-400">上行: <span class="text-orange-400 font-mono">{{
                        formatTraffic(aggregationData.wan.totalUpload) }}</span></span>
                      <span class="text-slate-400">下行: <span class="text-cyan-400 font-mono">{{
                        formatTraffic(aggregationData.wan.totalDownload) }}</span></span>
                      <span class="text-slate-400">TCP: <span class="text-slate-200 font-mono">{{
                        aggregationData.wan.totalTcp }}</span></span>
                      <span class="text-slate-400">UDP: <span class="text-slate-200 font-mono">{{
                        aggregationData.wan.totalUdp }}</span></span>
                      <span class="text-slate-400">其他: <span class="text-slate-200 font-mono">{{
                        aggregationData.wan.totalOther }}</span></span>
                    </div>
                  </div>
                </td>
              </tr>
              <!-- 外网IP详细行 -->
              <tr v-for="ipStats in aggregationData.wan.ips" :key="ipStats.ip" v-show="!uiState.ipGroupCollapsed.wan"
                class="hover:bg-slate-700/30 transition-colors">
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-300">{{ ipStats.ip }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.traffic.value.toFixed(2) }} {{ ipStats.traffic.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-orange-400">{{ ipStats.upload.value.toFixed(2) }} {{ ipStats.upload.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-cyan-400">{{ ipStats.download.value.toFixed(2) }} {{ ipStats.download.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.tcpCount }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.udpCount }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.otherCount }}</span>
                </td>
              </tr>
              <tr v-if="aggregationData.wan.ips.length === 0 && !uiState.ipGroupCollapsed.wan">
                <td colspan="7" class="px-5 py-4 text-center text-slate-500 text-xs">暂无外网IP数据</td>
              </tr>

              <!-- 未知IP分组 -->
              <tr class="bg-slate-700/30 hover:bg-slate-700/50 transition-colors cursor-pointer"
                @click="toggleIpGroup('unknown')">
                <td colspan="7" class="px-3 py-3 text-left">
                  <div class="flex items-center justify-between">
                    <div class="flex items-center gap-2">
                      <span class="text-slate-500 transition-transform duration-300"
                        :class="{ 'rotate-180': !uiState.ipGroupCollapsed.unknown }">▼</span>
                      <span class="font-semibold text-slate-200">{{ aggregationData.unknown.name }}</span>
                      <span class="text-xs text-slate-500">({{ aggregationData.unknown.ips.length }} 个 IP)</span>
                    </div>
                    <div class="flex items-center gap-4 text-xs">
                      <span class="text-slate-400">总流量: <span class="text-slate-200 font-mono">{{
                        formatTraffic(aggregationData.unknown.totalTraffic) }}</span></span>
                      <span class="text-slate-400">上行: <span class="text-orange-400 font-mono">{{
                        formatTraffic(aggregationData.unknown.totalUpload) }}</span></span>
                      <span class="text-slate-400">下行: <span class="text-cyan-400 font-mono">{{
                        formatTraffic(aggregationData.unknown.totalDownload) }}</span></span>
                      <span class="text-slate-400">TCP: <span class="text-slate-200 font-mono">{{
                        aggregationData.unknown.totalTcp }}</span></span>
                      <span class="text-slate-400">UDP: <span class="text-slate-200 font-mono">{{
                        aggregationData.unknown.totalUdp }}</span></span>
                      <span class="text-slate-400">其他: <span class="text-slate-200 font-mono">{{
                        aggregationData.unknown.totalOther }}</span></span>
                    </div>
                  </div>
                </td>
              </tr>
              <!-- 未知IP详细行 -->
              <tr v-for="ipStats in aggregationData.unknown.ips" :key="ipStats.ip"
                v-show="!uiState.ipGroupCollapsed.unknown" class="hover:bg-slate-700/30 transition-colors">
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-300">{{ ipStats.ip }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.traffic.value.toFixed(2) }} {{ ipStats.traffic.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-orange-400">{{ ipStats.upload.value.toFixed(2) }} {{ ipStats.upload.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-cyan-400">{{ ipStats.download.value.toFixed(2) }} {{ ipStats.download.unit
                    }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.tcpCount }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.udpCount }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{ ipStats.otherCount }}</span>
                </td>
              </tr>
              <tr v-if="aggregationData.unknown.ips.length === 0 && !uiState.ipGroupCollapsed.unknown">
                <td colspan="7" class="px-5 py-4 text-center text-slate-500 text-xs">暂无未知IP数据</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- 2. 连接列表 -->
    <div>
      <!-- 连接列表折叠栏 -->
      <div @click="toggleMainAccordion('connectionList')"
        class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
        <div class="flex items-center gap-4">
          <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">连接列表</h3>
        </div>
        <span class="text-slate-500 transition-transform duration-300"
          :class="{ 'rotate-180': uiState.accordions.connectionList }">▼</span>
      </div>

      <!-- 连接列表内容 -->
      <div v-show="uiState.accordions.connectionList"
        class="bg-slate-800 border border-slate-700 rounded-xl overflow-hidden">
        <!-- 全局搜索框（单独一行，居右）带清空按钮 -->
        <div class="px-4 py-3 border-b border-slate-700 flex justify-end">
          <div class="relative">
            <input v-model="globalFilter" placeholder="全局搜索..."
              class="bg-slate-900 border border-slate-600 text-white text-xs px-3 py-1.5 pr-8 rounded w-56 outline-none focus:border-blue-500" />
            <button v-if="globalFilter" @click="globalFilter = ''"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300 text-xs w-4 h-4 flex items-center justify-center rounded hover:bg-slate-700 transition-colors"
              title="清空搜索">
              ×
            </button>
          </div>
        </div>

        <div class="overflow-x-auto">
          <table class="w-full text-sm text-center border-collapse">
            <thead class="bg-slate-700/50 text-slate-300">
              <tr>
                <th v-for="header in table.getHeaderGroups()[0].headers" :key="header.id"
                  @click="header.column.getCanSort() ? header.column.toggleSorting(undefined, header.column.getIsSorted() === false) : null"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap" :class="{
                    'cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors': header.column.getCanSort(),
                  }">
                  <div class="flex items-center justify-center gap-1">
                    <FlexRender :render="header.column.columnDef.header" :props="header.getContext()" />
                    <span v-if="header.column.getCanSort()" class="text-slate-400">
                      {{ getConnectionSortIcon(header.column.id) }}
                    </span>
                  </div>
                  <!-- 列过滤器 -->
                  <div v-if="header.column.getCanFilter()" class="mt-1">
                    <input :value="header.column.getFilterValue() ?? ''"
                      @input="e => header.column.setFilterValue((e.target as HTMLInputElement).value)"
                      :placeholder="`过滤 ${header.column.columnDef.header as string}...`"
                      class="bg-slate-900 border border-slate-600 text-xs px-1 py-0.5 rounded w-24 text-slate-200 outline-none"
                      @click.stop />
                  </div>
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-700">
              <tr v-for="row in table.getRowModel().rows" :key="row.id" class="hover:bg-slate-700/30 transition-colors">
                <td v-for="cell in row.getVisibleCells()" :key="cell.id" class="px-3 py-2 text-center">
                  <FlexRender :render="cell.column.columnDef.cell" :props="cell.getContext()" />
                </td>
              </tr>
              <tr v-if="table.getRowModel().rows.length === 0">
                <td colspan="7" class="px-5 py-8 text-center text-slate-500">暂无匹配数据</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>
