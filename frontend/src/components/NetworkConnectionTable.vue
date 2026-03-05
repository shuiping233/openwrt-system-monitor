<script setup lang="ts">
import { ref, computed, h, watch, reactive, onMounted, onUnmounted, nextTick } from 'vue';
import {
  useVueTable,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  FlexRender,
  createColumnHelper,
  SortingState,
  ColumnFiltersState
} from '@tanstack/vue-table';
import type { ConnectionApiResponse, AggregationTrafficResponse, AggregationTrafficDetails, IpAddressType } from '../model';
import { compressIPv6 } from '../utils/ipv6';
import { convertToBytes, BytesFixed, formatIOBytes, normalizeToBytes, formatDataBytes } from '../utils/convert';
import { useToast } from '../useToast';
import { useDatabase } from '../useDatabase';
import { useSettings } from '../useSettings';
import { useDnsQuery } from '../useDnsQuery';

// Props
const props = defineProps<{
  connectionData?: ConnectionApiResponse;
  aggregationData?: AggregationTrafficResponse;
}>();

// Database
const { getAccordionState, setAccordionState } = useDatabase();

// DNS Query
const { queryDns } = useDnsQuery();
const { settings, setConfig: setConfig } = useSettings();

// DNS 缓存映射表
const dnsCache = ref<Map<string, string>>(new Map());

// 分别跟踪两个表格的查询状态
const aggregationQuerying = ref(false);
const connectionsQuerying = ref(false);

// 聚合统计 DNS 启用状态
const enableAggregationDns = computed({
  get: () => settings.enable_dns_query_aggregation,
  set: async (value) => {
    await setConfig('enable_dns_query_aggregation', value);
    if (value) {
      // 启用时立即查询当前显示的 IP
      queryAggregationDns();
    }
  }
});

// 连接列表 DNS 启用状态
const enableConnectionsDns = computed({
  get: () => settings.enable_dns_query_connections,
  set: async (value) => {
    await setConfig('enable_dns_query_connections', value);
    if (value) {
      // 启用时立即查询当前显示的 IP
      queryConnectionsDns();
    }
  }
});

// 获取 IP 显示文本（主机名或 IP）
const getIpDisplay = (ip: string): string => {
  return dnsCache.value.get(ip) || ip;
};

// 获取聚合统计表格中当前显示的 IP 地址
const getAggregationVisibleIps = (): string[] => {
  const ips: string[] = [];
  // 遍历所有分组的 IP
  for (const group of [aggregationData.value.lan, aggregationData.value.wan, aggregationData.value.unknown]) {
    for (const ipStats of group.ips) {
      if (!dnsCache.value.has(ipStats.ip)) {
        ips.push(ipStats.ip);
      }
    }
  }
  return ips;
};

// 查询聚合统计 DNS
const queryAggregationDns = async () => {
  if (!enableAggregationDns.value || aggregationQuerying.value) return;
  const ips = getAggregationVisibleIps();
  if (ips.length === 0) return;

  aggregationQuerying.value = true;
  try {
    const results = await queryDns(ips);
    for (const [ip, hostname] of results) {
      dnsCache.value.set(ip, hostname);
    }
  } finally {
    aggregationQuerying.value = false;
  }
};

// 获取连接列表表格中当前显示的 IP 地址（仅当前页）
const getConnectionsVisibleIps = (): string[] => {
  const ips: string[] = [];
  // table 在下方定义，使用 try-catch 避免初始化时出错
  try {
    const visibleRows = table.getPaginationRowModel().rows;
    for (const row of visibleRows) {
      const sourceIp = row.original.source_ip;
      const destIp = row.original.destination_ip;
      if (!dnsCache.value.has(sourceIp)) {
        ips.push(sourceIp);
      }
      if (!dnsCache.value.has(destIp)) {
        ips.push(destIp);
      }
    }
  } catch (e) {
    // table 尚未初始化
  }
  return [...new Set(ips)]; // 去重
};

// 查询连接列表 DNS
const queryConnectionsDns = async () => {
  if (!enableConnectionsDns.value || connectionsQuerying.value) return;
  const ips = getConnectionsVisibleIps();
  if (ips.length === 0) return;

  connectionsQuerying.value = true;
  try {
    const results = await queryDns(ips);
    for (const [ip, hostname] of results) {
      dnsCache.value.set(ip, hostname);
    }
  } finally {
    connectionsQuerying.value = false;
  }
};

// DNS 轮询定时器
let dnsPollInterval: number | null = null;

// 启动 DNS 轮询
const startDnsPolling = () => {
  if (dnsPollInterval) return;
  const intervalMs = settings.dns_poll_interval * 1000;
  dnsPollInterval = window.setInterval(() => {
    if (enableAggregationDns.value) {
      queryAggregationDns();
    }
    if (enableConnectionsDns.value) {
      queryConnectionsDns();
    }
  }, intervalMs);
};

// 停止 DNS 轮询
const stopDnsPolling = () => {
  if (dnsPollInterval) {
    clearInterval(dnsPollInterval);
    dnsPollInterval = null;
  }
};

// 监听 DNS 启用状态，启动/停止轮询
watch([enableAggregationDns, enableConnectionsDns], ([aggEnabled, connEnabled]) => {
  if (aggEnabled || connEnabled) {
    startDnsPolling();
    // 立即执行一次查询
    if (aggEnabled) queryAggregationDns();
    if (connEnabled) queryConnectionsDns();
  } else {
    stopDnsPolling();
  }
}, { immediate: true });

// 监听轮询间隔变化，重启轮询
watch(() => settings.dns_poll_interval, () => {
  if (enableAggregationDns.value || enableConnectionsDns.value) {
    stopDnsPolling();
    startDnsPolling();
  }
});

// 组件卸载时清理定时器
onUnmounted(() => {
  stopDnsPolling();
});

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

// ================= 3. 聚合统计排序状态 =================
type SortDirection = 'asc' | 'desc' | null;
type SortColumn = 'ip' | 'totalThroughput' | 'uploadThroughput' | 'downloadThroughput' | 'totalTraffic' | 'totalUpload' | 'totalDownload' | 'tcp' | 'udp' | 'other';

const aggregationSort = reactive<{
  column: SortColumn;
  direction: SortDirection;
}>({
  column: 'totalThroughput',
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
  totalThroughput: TrafficMetric;  // 总实时速率 - total_throughput
  uploadThroughput: TrafficMetric;   // 上行流量 - incoming
  downloadThroughput: TrafficMetric; // 下行流量 - outgoing
  totalTraffic: TrafficMetric;  // 累计上下行流量 - total_traffic
  totalUpload: TrafficMetric;   // 累计上行流量 - total_incoming
  totalDownload: TrafficMetric; // 累计下行流量 - total_outgoing
  tcpCount: number;
  udpCount: number;
  otherCount: number;
}

interface GroupStats {
  name: string;
  key: IpAddressType;
  ips: IPStats[];
  totalThroughput: number;
  UploadThroughput: number;
  DownloadThroughput: number;
  totalTraffic: number;
  totalUpload: number;
  totalDownload: number;
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
      case 'totalThroughput':
        comparison = a.totalThroughput.bytes - b.totalThroughput.bytes;
        break;
      case 'uploadThroughput':
        comparison = a.uploadThroughput.bytes - b.uploadThroughput.bytes;
        break;
      case 'downloadThroughput':
        comparison = a.downloadThroughput.bytes - b.downloadThroughput.bytes;
        break;
      case 'totalTraffic':
        comparison = a.totalTraffic.bytes - b.totalTraffic.bytes;
        break;
      case 'totalUpload':
        comparison = a.totalUpload.bytes - b.totalUpload.bytes;
        break;
      case 'totalDownload':
        comparison = a.totalDownload.bytes - b.totalDownload.bytes;
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
  const lowerFilter = filter.toLowerCase().replace(/\s+/g, '');
  return ips.filter(ip => {
    // 检查 IP 地址
    if (ip.ip.toLowerCase().includes(lowerFilter)) return true;
    
    // 检查 hostname（如果启用了 DNS）
    const hostname = dnsCache.value.get(ip.ip);
    if (hostname && hostname.toLowerCase().includes(lowerFilter)) return true;
    
    // 检查格式化后的流量值（数值+单位）
    const totalThroughputStr = BytesFixed(ip.totalThroughput.value, ip.totalThroughput.unit) + ip.totalThroughput.unit;
    const uploadThroughputStr = BytesFixed(ip.uploadThroughput.value, ip.uploadThroughput.unit) + ip.uploadThroughput.unit;
    const downloadThroughputStr = BytesFixed(ip.downloadThroughput.value, ip.downloadThroughput.unit) + ip.downloadThroughput.unit;
    const totalTrafficStr = BytesFixed(ip.totalTraffic.value, ip.totalTraffic.unit) + ip.totalTraffic.unit;
    const totalUploadStr = BytesFixed(ip.totalUpload.value, ip.totalUpload.unit) + ip.totalUpload.unit;
    const totalDownloadStr = BytesFixed(ip.totalDownload.value, ip.totalDownload.unit) + ip.totalDownload.unit;
    
    if (totalThroughputStr.toLowerCase().replace(/\s+/g, '').includes(lowerFilter)) return true;
    if (uploadThroughputStr.toLowerCase().replace(/\s+/g, '').includes(lowerFilter)) return true;
    if (downloadThroughputStr.toLowerCase().replace(/\s+/g, '').includes(lowerFilter)) return true;
    if (totalTrafficStr.toLowerCase().replace(/\s+/g, '').includes(lowerFilter)) return true;
    if (totalUploadStr.toLowerCase().replace(/\s+/g, '').includes(lowerFilter)) return true;
    if (totalDownloadStr.toLowerCase().replace(/\s+/g, '').includes(lowerFilter)) return true;
    
    // 检查原始数值
    if (String(ip.totalThroughput.value).includes(lowerFilter)) return true;
    if (String(ip.uploadThroughput.value).includes(lowerFilter)) return true;
    if (String(ip.downloadThroughput.value).includes(lowerFilter)) return true;
    if (String(ip.totalTraffic.value).includes(lowerFilter)) return true;
    if (String(ip.totalUpload.value).includes(lowerFilter)) return true;
    if (String(ip.totalDownload.value).includes(lowerFilter)) return true;
    
    // 检查单位
    if (ip.totalThroughput.unit.toLowerCase().includes(lowerFilter)) return true;
    if (ip.uploadThroughput.unit.toLowerCase().includes(lowerFilter)) return true;
    if (ip.downloadThroughput.unit.toLowerCase().includes(lowerFilter)) return true;
    if (ip.totalTraffic.unit.toLowerCase().includes(lowerFilter)) return true;
    if (ip.totalUpload.unit.toLowerCase().includes(lowerFilter)) return true;
    if (ip.totalDownload.unit.toLowerCase().includes(lowerFilter)) return true;
    
    // 检查连接数
    if (String(ip.tcpCount).includes(lowerFilter)) return true;
    if (String(ip.udpCount).includes(lowerFilter)) return true;
    if (String(ip.otherCount).includes(lowerFilter)) return true;
    
    return false;
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
    totalThroughput: {
      value: detail.total_throughput.value,
      unit: detail.total_throughput.unit,
      bytes: metricUnitToBytes(detail.total_throughput),
    },
    uploadThroughput: {
      value: detail.outgoing.value,
      unit: detail.outgoing.unit,
      bytes: metricUnitToBytes(detail.outgoing),
    },
    downloadThroughput: {
      value: detail.incoming.value,
      unit: detail.incoming.unit,
      bytes: metricUnitToBytes(detail.incoming),
    },
    totalTraffic: {
      value: detail.total_traffic.value,
      unit: detail.total_traffic.unit,
      bytes: metricUnitToBytes(detail.total_traffic),
    },
    totalUpload: {
      value: detail.total_incoming.value,
      unit: detail.total_incoming.unit,
      bytes: metricUnitToBytes(detail.total_incoming),
    },
    totalDownload: {
      value: detail.total_outgoing.value,
      unit: detail.total_outgoing.unit,
      bytes: metricUnitToBytes(detail.total_outgoing),
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
      totalThroughput: ips.reduce((sum, ip) => sum + ip.totalThroughput.bytes, 0),
      UploadThroughput: ips.reduce((sum, ip) => sum + ip.uploadThroughput.bytes, 0),
      DownloadThroughput: ips.reduce((sum, ip) => sum + ip.downloadThroughput.bytes, 0),
      totalTraffic: ips.reduce((sum, ip) => sum + ip.totalTraffic.bytes, 0),
      totalUpload: ips.reduce((sum, ip) => sum + ip.totalUpload.bytes, 0),
      totalDownload: ips.reduce((sum, ip) => sum + ip.totalDownload.bytes, 0),
      totalTcp: ips.reduce((sum, ip) => sum + (ip.tcpCount >= 0 ? ip.tcpCount : 0), 0),
      totalUdp: ips.reduce((sum, ip) => sum + (ip.udpCount >= 0 ? ip.udpCount : 0), 0),
      totalOther: ips.reduce((sum, ip) => sum + (ip.otherCount >= 0 ? ip.otherCount : 0), 0),
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
const formatThroughput = (bytes: number): string => {
  if (bytes === 0) return '0 B';
  return formatIOBytes(bytes);
};

// 格式化流量显示
const formatTraffic = (bytes: number): string => {
  if (bytes === 0) return '0 B';
  return formatDataBytes(bytes);
};

// 复制功能
const copyInfo = (row: any) => {
  let source_ip: string = row.source_ip
  let destination_ip: string = row.destination_ip

  if (row.ip_family?.toUpperCase() === 'IPV6') {
    source_ip = `[${compressIPv6(row.source_ip)}]`;
    destination_ip = `[${compressIPv6(row.destination_ip)}]`;
  }

  const text = `[${row.ip_family}] ${row.protocol} ${source_ip}:${row.source_port} -> ${destination_ip}:${row.destination_port} | 状态: ${row.state || '-'} | 流量: ${BytesFixed(row.traffic.value, row.traffic.unit)} ${row.traffic.unit} (${row.packets} Pkgs)`;

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
    cell: (info) => {
      const protocol = info.getValue()?.toUpperCase();
      const colorClass = protocol === 'TCP' ? 'text-blue-400' : protocol === 'UDP' ? 'text-violet-400' : 'text-slate-200';
      return h('span', { class: `bg-slate-700 px-2 py-1 rounded text-xs ${colorClass}` }, protocol);
    },
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
      const displayIp = dnsCache.value.get(ip) || formatIP(ip, row.ip_family);
      const fullText = displayIp + (port > 0 ? ':' + port : '');
      return h('span', {
        class: 'font-mono text-slate-300',
        title: formatIP(ip, row.ip_family)
      }, fullText);
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
      const searchStr = filterValue.toLowerCase();
      // 检查 IP 和端口
      if (fullAddress.toLowerCase().includes(searchStr)) return true;
      // 检查 hostname
      const hostname = dnsCache.value.get(ip as string);
      if (hostname && hostname.toLowerCase().includes(searchStr)) return true;
      return false;
    },
  }),
  // 目标地址
  columnHelper.accessor('destination_ip', {
    header: '目标地址',
    cell: (info) => {
      const row = info.row.original;
      const ip = info.getValue();
      const port = row.destination_port;
      const displayIp = dnsCache.value.get(ip) || formatIP(ip, row.ip_family);
      const fullText = displayIp + (port > 0 ? ':' + port : '');
      return h('span', {
        class: 'font-mono text-slate-300',
        title: formatIP(ip, row.ip_family)
      }, fullText);
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
      const searchStr = filterValue.toLowerCase();
      // 检查 IP 和端口
      if (fullAddress.toLowerCase().includes(searchStr)) return true;
      // 检查 hostname
      const hostname = dnsCache.value.get(ip as string);
      if (hostname && hostname.toLowerCase().includes(searchStr)) return true;
      return false;
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

// ================= 分页相关配置 =================
// 注意：settings 和 setConfig 已在前面声明

// 分页大小选项
const pageSizeOptions = [20, 50, 100, 500, 1000];

// 分页大小 - 从配置读取
const pageSize = ref(settings.network_table_page_size || pageSizeOptions[0]);

// 是否是自定义分页大小
const isCustomPageSize = ref(!pageSizeOptions.includes(pageSize.value));

// 自定义分页大小输入值
const customPageSize = ref(isCustomPageSize.value ? String(pageSize.value) : '');

// 当前页码（从0开始）
const currentPage = ref(0);

// 用户期望的页码（用于数据刷新时保持页码）
const desiredPageIndex = ref(0);

// 页码输入框的值
const pageInputValue = ref('1');

// 受控分页状态 - 必须在 table 和 watch 之前定义
const pagination = ref({
  pageSize: pageSize.value,
  pageIndex: currentPage.value,
});

// 当配置加载完成后，同步分页大小
watch(() => settings.network_table_page_size, (newValue) => {
  if (newValue && newValue !== pageSize.value) {
    pageSize.value = newValue;
    isCustomPageSize.value = !pageSizeOptions.includes(newValue);
    if (isCustomPageSize.value) {
      customPageSize.value = String(newValue);
    }
    // 同步更新 pagination 的 pageSize
    pagination.value = {
      ...pagination.value,
      pageSize: newValue,
    };
  }
}, { immediate: true });

// 同步页码输入框与当前页
watch(currentPage, (newPage) => {
  pageInputValue.value = String(newPage + 1);
}, { immediate: true });

// 跳转到指定页
const jumpToPage = () => {
  const targetPage = parseInt(pageInputValue.value, 10);
  if (isNaN(targetPage) || targetPage < 1) {
    // 无效输入，重置为当前页
    pageInputValue.value = String(currentPage.value + 1);
    return;
  }
  const totalPages = table.getPageCount();
  if (totalPages === 0) return;

  // 限制在有效范围内
  const validPage = Math.min(Math.max(targetPage, 1), totalPages);
  const newIndex = validPage - 1;

  desiredPageIndex.value = newIndex;
  pagination.value = {
    ...pagination.value,
    pageIndex: newIndex,
  };
  currentPage.value = newIndex;
  pageInputValue.value = String(validPage);
};

// 处理分页大小变更
const handlePageSizeChange = async (value: string) => {
  const newSize = parseInt(value, 10);
  if (!isNaN(newSize) && newSize > 0) {
    pageSize.value = newSize;
    // 切换分页大小时保持当前页码，但确保页码有效
    const totalRows = table.getFilteredRowModel().rows.length;
    const totalPages = Math.ceil(totalRows / newSize);
    const newPageIndex = totalPages > 0 ? Math.min(desiredPageIndex.value, totalPages - 1) : 0;
    desiredPageIndex.value = newPageIndex;
    currentPage.value = newPageIndex;
    pagination.value = {
      pageSize: newSize,
      pageIndex: newPageIndex,
    };
    await setConfig('network_table_page_size', newSize);
  }
};

// 处理自定义分页大小变更
const handleCustomPageSizeChange = async () => {
  const value = parseInt(customPageSize.value, 10);
  if (!isNaN(value) && value > 0) {
    pageSize.value = value;
    // 切换分页大小时保持当前页码，但确保页码有效
    const totalRows = table.getFilteredRowModel().rows.length;
    const totalPages = Math.ceil(totalRows / value);
    const newPageIndex = totalPages > 0 ? Math.min(desiredPageIndex.value, totalPages - 1) : 0;
    desiredPageIndex.value = newPageIndex;
    currentPage.value = newPageIndex;
    pagination.value = {
      pageSize: value,
      pageIndex: newPageIndex,
    };
    await setConfig('network_table_page_size', value);
  }
};

// 切换到预设分页大小
const switchToPresetSize = async (size: number) => {
  pageSize.value = size;
  isCustomPageSize.value = false;
  customPageSize.value = '';
  // 切换分页大小时保持当前页码，但确保页码有效
  const totalRows = table.getFilteredRowModel().rows.length;
  const totalPages = Math.ceil(totalRows / size);
  const newPageIndex = totalPages > 0 ? Math.min(desiredPageIndex.value, totalPages - 1) : 0;
  desiredPageIndex.value = newPageIndex;
  currentPage.value = newPageIndex;
  pagination.value = {
    pageSize: size,
    pageIndex: newPageIndex,
  };
  await setConfig('network_table_page_size', size);
};

// 监听数据变化，仅处理页码越界的情况
watch(displayData, () => {
  // 使用 nextTick 确保 TanStack Table 已经处理了数据变化
  nextTick(() => {
    const totalPages = Math.max(1, table.getPageCount());
    const currentIndex = pagination.value.pageIndex;

    // 如果当前页码超过最大页数，跳到最后一页
    if (currentIndex >= totalPages) {
      const newIndex = totalPages - 1;
      desiredPageIndex.value = newIndex;
      pagination.value = {
        ...pagination.value,
        pageIndex: newIndex,
      };
      currentPage.value = newIndex;
    }
  });
}, { immediate: true });

// 初始状态 - 只允许同时排列一行
const initialSorting = [{ id: 'traffic', desc: true }] as SortingState;

// 列过滤器状态
const columnFilters = ref<ColumnFiltersState>([]);

const table = useVueTable({
  data: displayData,
  columns,
  getCoreRowModel: getCoreRowModel(),
  getSortedRowModel: getSortedRowModel(),
  getFilteredRowModel: getFilteredRowModel(),
  getPaginationRowModel: getPaginationRowModel(),
  enableMultiSort: false, // 只允许同时排列一行
  autoResetPageIndex: false, // 禁用数据变化时自动重置到第一页
  getRowId: (row, index, parent) => {
    // 为每个连接创建一个标准化的唯一ID
    const endpointA = `${row.source_ip}:${row.source_port}`;
    const endpointB = `${row.destination_ip}:${row.destination_port}`;
    const endpoints = [endpointA, endpointB].sort(); // 排序确保一致性
    const baseId = `${endpoints[0]}<->${endpoints[1]}-${row.protocol}`;

    // 添加一个稳定的唯一标识符，基于连接信息和原始索引
    return `${baseId}-${row.traffic.value}-${row.packets}-${index}`;
  },
  state: {
    get pagination() {
      return pagination.value;
    },
    get columnFilters() {
      return columnFilters.value;
    },
    get globalFilter() {
      return globalFilter.value;
    },
    sorting: initialSorting,
  },
  globalFilterFn: (row, columnId, value) => {
    const search = String(value).toLowerCase().replace(/\s+/g, '');
    const original = row.original;
    
    // 构建要搜索的字符串数组
    const searchFields: string[] = [];
    
    // 基础字段
    searchFields.push(original.ip_family || '');
    searchFields.push(original.protocol || '');
    searchFields.push(original.source_ip || '');
    searchFields.push(String(original.source_port || ''));
    searchFields.push(original.destination_ip || '');
    searchFields.push(String(original.destination_port || ''));
    searchFields.push(original.state || '');
    
    // 添加 hostname（如果 DNS 已查询）
    const sourceHostname = dnsCache.value.get(original.source_ip);
    const destHostname = dnsCache.value.get(original.destination_ip);
    if (sourceHostname) searchFields.push(sourceHostname);
    if (destHostname) searchFields.push(destHostname);
    
    // 格式化后的流量值（数值+单位）
    const trafficValue = original.traffic?.value || 0;
    const trafficUnit = original.traffic?.unit || 'B';
    const formattedTraffic = BytesFixed(trafficValue, trafficUnit) + trafficUnit;
    searchFields.push(formattedTraffic);
    searchFields.push(String(trafficValue));
    searchFields.push(trafficUnit);
    
    // 包数量
    searchFields.push(String(original.packets || ''));
    
    // 合并并搜索
    const rowStr = searchFields.join(' ').toLowerCase().replace(/\s+/g, '');
    return rowStr.includes(search);
  },
  onPaginationChange: (updater) => {
    const newPagination = typeof updater === 'function'
      ? updater(pagination.value)
      : updater;
    desiredPageIndex.value = newPagination.pageIndex;
    pagination.value = newPagination;
    currentPage.value = newPagination.pageIndex;
  },
  onColumnFiltersChange: (updater) => {
    const newFilters = typeof updater === 'function'
      ? updater(columnFilters.value)
      : updater;
    columnFilters.value = newFilters;
  },
  onGlobalFilterChange: (updater) => {
    const newFilter = typeof updater === 'function'
      ? updater(globalFilter.value)
      : updater;
    globalFilter.value = newFilter;
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
        class="bg-slate-800 border border-slate-700 rounded-xl p-5 border-t-4 border-t-blue-400 flex items-center justify-between">
        <div>
          <div class="text-slate-400 text-sm">TCP 连接</div>
          <div class="text-3xl font-bold">{{ connectionData?.counts?.tcp || 0 }}</div>
        </div>
        <div class="text-blue-400/20 text-4xl">T</div>
      </div>
      <div
        class="bg-slate-800 border border-slate-700 rounded-xl p-5 border-t-4 border-t-violet-400 flex items-center justify-between">
        <div>
          <div class="text-slate-400 text-sm">UDP 连接</div>
          <div class="text-3xl font-bold">{{ connectionData?.counts?.udp || 0 }}</div>
        </div>
        <div class="text-violet-400/20 text-4xl">U</div>
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
        <!-- DNS 查询开关 + 搜索框 -->
        <div class="px-4 py-3 border-b border-slate-700 flex items-center justify-between">
          <!-- DNS 查询开关（居左） -->
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" v-model="enableAggregationDns"
              class="w-4 h-4 rounded border-slate-600 text-blue-500 focus:ring-blue-500 bg-slate-700" />
            <span class="text-sm text-slate-300">启用 DNS 查询</span>
            <span v-if="aggregationQuerying" class="text-xs text-blue-400 animate-pulse">查询中...</span>
          </label>
          <!-- 全局搜索框（居右） -->
          <div class="relative">
            <input v-model="aggregationFilter" placeholder="搜索 IP、流量、连接数..."
              class="bg-slate-900 border border-slate-600 text-white text-xs px-3 py-1.5 pr-8 rounded w-56 outline-none focus:border-blue-400" />
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
                <th @click="toggleAggregationSort('totalThroughput')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    总实时速率
                    <span class="text-slate-400">{{ getSortIcon('totalThroughput') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('uploadThroughput')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    实时上行
                    <span class="text-slate-400">{{ getSortIcon('uploadThroughput') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('downloadThroughput')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    实时下行
                    <span class="text-slate-400">{{ getSortIcon('downloadThroughput') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('totalTraffic')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    累计上下行流量
                    <span class="text-slate-400">{{ getSortIcon('totalTraffic') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('totalUpload')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    累计上行流量
                    <span class="text-slate-400">{{ getSortIcon('totalUpload') }}</span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('totalDownload')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex items-center justify-center gap-1">
                    累计下行流量
                    <span class="text-slate-400">{{ getSortIcon('totalDownload') }}</span>
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
              <template v-for="group in [aggregationData.lan, aggregationData.wan, aggregationData.unknown]"
                :key="group.key">
                <!-- 分组标题行 -->
                <tr class="bg-slate-700/30 hover:bg-slate-700/50 transition-colors cursor-pointer"
                  @click="toggleIpGroup(group.key)">
                  <td colspan="10" class="px-3 py-3 text-left">
                    <div class="flex items-center justify-between">
                      <div class="flex items-center gap-2">
                        <span class="text-slate-500 transition-transform duration-300"
                          :class="{ 'rotate-180': !uiState.ipGroupCollapsed[group.key] }">▼</span>
                        <span class="font-semibold text-slate-200">{{ group.name }}</span>
                        <span class="text-xs text-slate-500">({{ group.ips.length }} 个 IP)</span>
                      </div>
                      <div class="flex items-center gap-4 text-xs">
                        <span class="text-slate-400">总实时速率: <span class="text-slate-200 font-mono">{{
                          formatThroughput(group.totalThroughput) }}</span></span>
                        <span class="text-slate-400">上行速率: <span class="text-orange-400 font-mono">{{
                          formatThroughput(group.UploadThroughput) }}</span></span>
                        <span class="text-slate-400">下行速率: <span class="text-cyan-400 font-mono">{{
                          formatThroughput(group.DownloadThroughput) }}</span></span>
                        <span class="text-slate-400">累计上下行流量: <span class="text-slate-200 font-mono">{{
                          formatTraffic(group.totalTraffic) }}</span></span>
                        <span class="text-slate-400">累计上行流量: <span class="text-orange-400 font-mono">{{
                          formatTraffic(group.totalUpload) }}</span></span>
                        <span class="text-slate-400">累计下行流量: <span class="text-cyan-400 font-mono">{{
                          formatTraffic(group.totalDownload) }}</span></span>
                        <span class="text-slate-400">TCP: <span class="text-blue-400 font-mono">{{
                          group.totalTcp }}</span></span>
                        <span class="text-slate-400">UDP: <span class="text-violet-400 font-mono">{{
                          group.totalUdp }}</span></span>
                        <span class="text-slate-400">其他: <span class="text-slate-200 font-mono">{{
                          group.totalOther }}</span></span>
                      </div>
                    </div>
                  </td>
                </tr>
                <!-- 分组详细行 -->
                <tr v-for="ipStats in group.ips" :key="ipStats.ip" v-show="!uiState.ipGroupCollapsed[group.key]"
                  class="hover:bg-slate-700/30 transition-colors">
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-slate-300" :title="ipStats.ip">{{ getIpDisplay(ipStats.ip) }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-slate-200">{{
                      BytesFixed(ipStats.totalThroughput.value, ipStats.totalThroughput.unit) }} {{
                        ipStats.totalThroughput.unit
                      }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-orange-400">{{
                      BytesFixed(ipStats.uploadThroughput.value, ipStats.uploadThroughput.unit) }} {{
                        ipStats.uploadThroughput.unit
                      }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-cyan-400">{{
                      BytesFixed(ipStats.downloadThroughput.value, ipStats.downloadThroughput.unit) }} {{
                        ipStats.downloadThroughput.unit
                      }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-slate-200">{{
                      BytesFixed(ipStats.totalTraffic.value, ipStats.totalTraffic.unit) }} {{
                        ipStats.totalTraffic.unit
                      }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-orange-400">{{
                      BytesFixed(ipStats.totalUpload.value, ipStats.totalUpload.unit) }} {{
                        ipStats.totalUpload.unit
                      }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-cyan-400">{{
                      BytesFixed(ipStats.totalDownload.value, ipStats.totalDownload.unit) }} {{
                        ipStats.totalDownload.unit
                      }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-blue-400">{{ ipStats.tcpCount }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-violet-400">{{ ipStats.udpCount }}</span>
                  </td>
                  <td class="px-3 py-2 text-center">
                    <span class="font-mono text-slate-200">{{ ipStats.otherCount }}</span>
                  </td>
                </tr>
                <!-- 空数据提示 -->
                <tr v-if="group.ips.length === 0 && !uiState.ipGroupCollapsed[group.key]">
                  <td colspan="10" class="px-5 py-4 text-center text-slate-500 text-xs">暂无{{ group.name }}数据</td>
                </tr>
              </template>
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
        <!-- DNS 查询开关 + 搜索框 -->
        <div class="px-4 py-3 border-b border-slate-700 flex items-center justify-between">
          <!-- DNS 查询开关（居左） -->
          <label class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" v-model="enableConnectionsDns"
              class="w-4 h-4 rounded border-slate-600 text-blue-500 focus:ring-blue-500 bg-slate-700" />
            <span class="text-sm text-slate-300">启用 DNS 查询</span>
            <span v-if="connectionsQuerying" class="text-xs text-blue-400 animate-pulse">查询中...</span>
          </label>
          <!-- 全局搜索框（居右） -->
          <div class="relative">
            <input v-model="globalFilter" placeholder="全局搜索..."
              class="bg-slate-900 border border-slate-600 text-white text-xs px-3 py-1.5 pr-8 rounded w-56 outline-none focus:border-blue-400" />
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
              <tr v-for="row in table.getPaginationRowModel().rows" :key="row.id"
                class="hover:bg-slate-700/30 transition-colors">
                <td v-for="cell in row.getVisibleCells()" :key="cell.id" class="px-3 py-2 text-center">
                  <FlexRender :render="cell.column.columnDef.cell" :props="cell.getContext()" />
                </td>
              </tr>
              <tr v-if="table.getPaginationRowModel().rows.length === 0">
                <td colspan="7" class="px-5 py-8 text-center text-slate-500">暂无匹配数据</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- 分页控件 -->
        <div class="px-4 py-3 border-t border-slate-700 flex flex-wrap items-center justify-between gap-3">
          <!-- 左侧：分页大小选择器 -->
          <div class="flex items-center gap-2">
            <span class="text-xs text-slate-400">每页显示：</span>
            <!-- 预设分页大小按钮 -->
            <button v-for="size in pageSizeOptions" :key="size" @click="switchToPresetSize(size)"
              class="text-xs px-2 py-1 rounded transition-colors" :class="{
                'bg-blue-600 text-white': !isCustomPageSize && pageSize === size,
                'bg-slate-700 text-slate-300 hover:bg-slate-600': isCustomPageSize || pageSize !== size
              }">
              {{ size }}
            </button>
            <!-- 自定义输入框 -->
            <div class="flex items-center gap-1">
              <input v-model="customPageSize" type="number" min="1" placeholder="自定义"
                class="w-16 text-xs px-2 py-1 rounded bg-slate-900 border border-slate-600 text-white outline-none focus:border-blue-400 text-center"
                :class="{ 'border-blue-400': isCustomPageSize }" @change="handleCustomPageSizeChange"
                @keyup.enter="handleCustomPageSizeChange" />
              <span class="text-xs text-slate-400">条</span>
            </div>
          </div>

          <!-- 右侧：页码导航 -->
          <div class="flex items-center gap-3">
            <span class="text-xs text-slate-400">
              共 {{ table.getPageCount() }} 页，{{ table.getFilteredRowModel().rows.length }} 条记录
            </span>
            <div class="flex items-center gap-1">
              <button @click="table.previousPage()" :disabled="!table.getCanPreviousPage()"
                class="text-xs px-3 py-1 rounded bg-slate-700 text-slate-300 hover:bg-slate-600 disabled:opacity-50 disabled:cursor-not-allowed transition-colors">
                上一页
              </button>
              <div class="flex items-center gap-1 px-2">
                <input v-model="pageInputValue" type="number" min="1" :max="table.getPageCount() || 1"
                  class="w-15 text-xs px-2 py-1 rounded bg-slate-900 border border-slate-600 text-white outline-none focus:border-blue-400 text-left"
                  @change="jumpToPage" @keyup.enter="jumpToPage" />
                <span class="text-xs text-slate-400">/ {{ table.getPageCount() || 1 }}</span>
              </div>
              <button @click="table.nextPage()" :disabled="!table.getCanNextPage()"
                class="text-xs px-3 py-1 rounded bg-slate-700 text-slate-300 hover:bg-slate-600 disabled:opacity-50 disabled:cursor-not-allowed transition-colors">
                下一页
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
