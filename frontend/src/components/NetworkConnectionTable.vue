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
import type { ConnectionApiResponse, AggregationTrafficResponse, AggregationTrafficDetails, IpAddressType, IpFamilyType } from '../model';
import { IpAddressTypeList } from '../model';
import { convertToBytes, formatMetric, formatIOBytes, normalizeToBytes, formatDataBytes } from '../utils/convert';
import { useToast } from '../useToast';
import { useDatabase } from '../useDatabase';
import { useSettings } from '../useSettings';
import { useDnsQuery } from '../useDnsQuery';
import PaginationControls from './PaginationControls.vue';

// Props
const props = defineProps<{
  connectionData?: ConnectionApiResponse;
  aggregationData?: AggregationTrafficResponse;
}>();

// Database
const { getAccordionState, setAccordionState, getNavState, setNavState } = useDatabase();

// DNS Query
const { queryDns, getCachedHostname } = useDnsQuery();
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
    // 查询逻辑由 watch 处理，这里不需要重复调用
  }
});

// 连接列表 DNS 启用状态
const enableConnectionsDns = computed({
  get: () => settings.enable_dns_query_connections,
  set: async (value) => {
    await setConfig('enable_dns_query_connections', value);
    // 查询逻辑由 watch 处理，这里不需要重复调用
  }
});

// 获取 IP 显示文本（主机名或 IP）
// 优先查全局 DNS 缓存（切换 tab 后缓存仍然有效），其次查组件本地缓存
const getIpDisplay = (ip: string): string => {
  return getCachedHostname(ip) || dnsCache.value.get(ip) || ip;
};

const getIpv6Display = (ip: string, ipv6Prefix: boolean = false): string => {
  const hostname = getIpDisplay(ip);
  // 如果 hostname 和 ip 一样，说明没找到别名，显示为 [IPv6]
  if (hostname === ip) {
    return `[${ip}]`;
  }
  // 如果找到了别名，显示别名，根据需要决定是否加前缀
  return ipv6Prefix ? `(IPV6)${hostname}` : hostname;
};

// 获取连接列表表格中当前显示的 IP 地址（仅当前页）
// 优先返回未在全局缓存中的IP（需要查询的）
const getConnectionsVisibleIps = (): { all: string[]; needsQuery: string[] } => {
  const allIps: string[] = [];
  const needsQueryIps: string[] = [];

  // table 在下方定义，使用 try-catch 避免初始化时出错
  try {
    const visibleRows = table.getPaginationRowModel().rows;
    for (const row of visibleRows) {
      const sourceIp = row.original.source_ip;
      const destIp = row.original.destination_ip;

      allIps.push(sourceIp);
      allIps.push(destIp);

      // 检查是否在全局缓存中（使用 getCachedHostname 检查缓存是否过期）
      const sourceCached = getCachedHostname(sourceIp);
      const destCached = getCachedHostname(destIp);

      if (!sourceCached && !dnsCache.value.has(sourceIp)) {
        needsQueryIps.push(sourceIp);
      }
      if (!destCached && !dnsCache.value.has(destIp)) {
        needsQueryIps.push(destIp);
      }
    }
  } catch (e) {
    // table 尚未初始化
  }

  return {
    all: [...new Set(allIps)],
    needsQuery: [...new Set(needsQueryIps)]
  };
};

// 查询连接列表 DNS
// 优先查询未缓存的IP，然后查询所有IP
const queryConnectionsDns = async (forceQueryAll = false) => {
  if (!enableConnectionsDns.value || connectionsQuerying.value) return;
  const { all, needsQuery } = getConnectionsVisibleIps();

  // 如果没有需要查询的IP，且不是强制查询全部，则直接返回
  if (needsQuery.length === 0 && !forceQueryAll) return;

  connectionsQuerying.value = true;
  try {
    // 优先批量查询需要查询的IP（缓存过期或未请求的）
    if (needsQuery.length > 0) {
      const priorityResults = await queryDns(needsQuery);
      for (const [ip, hostname] of priorityResults) {
        dnsCache.value.set(ip, hostname);
      }
    }

    // 如果需要查询全部（初始加载时），再查询其他IP
    if (forceQueryAll && all.length > needsQuery.length) {
      const remainingIps = all.filter(ip => !needsQuery.includes(ip));
      if (remainingIps.length > 0) {
        const otherResults = await queryDns(remainingIps);
        for (const [ip, hostname] of otherResults) {
          dnsCache.value.set(ip, hostname);
        }
      }
    }
  } finally {
    connectionsQuerying.value = false;
  }
};

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

// 聚合统计分页大小选项（必须在前面定义，后续会用到）
const aggregationPageSizeOptions = [10, 20, 50, 100];

// 聚合统计当前激活的Tab
const activeAggregationTab = ref<IpAddressType>('lan');

// 聚合统计各Tab的分页状态
const aggregationPageStates = reactive<Record<IpAddressType, {
  pageSize: number;
  isCustomPageSize: boolean;
  customPageSize: string;
  currentPage: number;
  pageInputValue: string;
}>>({
  lan: {
    pageSize: settings.aggregation_table_page_size || aggregationPageSizeOptions[0],
    isCustomPageSize: !aggregationPageSizeOptions.includes(settings.aggregation_table_page_size),
    customPageSize: !aggregationPageSizeOptions.includes(settings.aggregation_table_page_size) ? String(settings.aggregation_table_page_size) : '',
    currentPage: 0,
    pageInputValue: '1',
  },
  wan: {
    pageSize: settings.aggregation_table_page_size || aggregationPageSizeOptions[0],
    isCustomPageSize: !aggregationPageSizeOptions.includes(settings.aggregation_table_page_size),
    customPageSize: !aggregationPageSizeOptions.includes(settings.aggregation_table_page_size) ? String(settings.aggregation_table_page_size) : '',
    currentPage: 0,
    pageInputValue: '1',
  },
  unknown: {
    pageSize: settings.aggregation_table_page_size || aggregationPageSizeOptions[0],
    isCustomPageSize: !aggregationPageSizeOptions.includes(settings.aggregation_table_page_size),
    customPageSize: !aggregationPageSizeOptions.includes(settings.aggregation_table_page_size) ? String(settings.aggregation_table_page_size) : '',
    currentPage: 0,
    pageInputValue: '1',
  },
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

  // 加载聚合统计当前选中的Tab
  const savedTab = await getNavState('network_connection_aggregation_active_tab');
  if (savedTab !== undefined && savedTab) {
    const validTab = savedTab as IpAddressType;
    if (IpAddressTypeList.includes(validTab)) {
      activeAggregationTab.value = validTab;
    }
  }
});

// 切换主折叠状态
const toggleMainAccordion = async (key: 'aggregation' | 'connectionList') => {
  uiState.accordions[key] = !uiState.accordions[key];
  const storageKey = key === 'aggregation' ? 'network_connection_aggregation' : 'network_connection_list';
  await setAccordionState(storageKey, uiState.accordions[key]);
};

// 格式化流量统计起始时间
const formatCaptureStartTime = (isoString: string | undefined): string => {
  if (!isoString) return '-';
  try {
    const date = new Date(isoString);
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });
  } catch {
    return isoString;
  }
};

// 根据总实时速率单位获取背景色类（夜晚友好）
const getThroughputBgClass = (unit: string): string => {
  const upperUnit = unit.toUpperCase();

  switch (upperUnit) {
    case 'B/S':
      return ''; // 初始状态，无背景
    case 'KB/S':
      // 起始：低调的深灰色
      return 'bg-slate-700/40';
    case 'MB/S':
      // 淡蓝色：像冰块一样的清透感
      return 'bg-blue-400/10 text-blue-300 border border-blue-400/20';

    case 'GB/S':
      // 淡黄色：柔和的奶油黄，不刺眼
      return 'bg-yellow-400/10 text-yellow-200 border border-yellow-400/20';

    case 'TB/S':
      // 淡红色：樱花粉/淡珊瑚色，优雅的警告
      return 'bg-red-400/15 text-red-300 border border-red-400/25';
    default:
      return 'bg-red-400/15 text-red-300 border border-red-400/25';
  }
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
  ipFamily: IpFamilyType;
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
    let hostname = ip.ip;
    if (ip.ipFamily.toLowerCase() === 'ipv6') {
      hostname = getIpv6Display(ip.ip, true);
    } else {
      hostname = getIpDisplay(ip.ip);
    }
    if (hostname && hostname.toLowerCase().includes(lowerFilter)) return true;

    // 检查格式化后的流量值（数值+单位）
    const totalThroughputStr = formatMetric(ip.totalThroughput.value, ip.totalThroughput.unit);
    const uploadThroughputStr = formatMetric(ip.uploadThroughput.value, ip.uploadThroughput.unit);
    const downloadThroughputStr = formatMetric(ip.downloadThroughput.value, ip.downloadThroughput.unit);
    const totalTrafficStr = formatMetric(ip.totalTraffic.value, ip.totalTraffic.unit);
    const totalUploadStr = formatMetric(ip.totalUpload.value, ip.totalUpload.unit);
    const totalDownloadStr = formatMetric(ip.totalDownload.value, ip.totalDownload.unit);

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

const aggregationData = computed((): { capture_start_at: string, lan: GroupStats; wan: GroupStats; unknown: GroupStats } => {
  // 从API数据中提取所有details
  let allDetails: AggregationTrafficDetails[] = [];

  if (props.aggregationData?.details) {
    allDetails = props.aggregationData.details;
  }

  // 转换为IPStats结构
  const ipStatsList: IPStats[] = allDetails.map((detail) => ({
    ip: detail.ip,
    ipType: detail.ip_type,
    ipFamily: detail.ip_family,
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
    capture_start_at: props.aggregationData.capture_start_at,
    lan: calculateGroupTotal(lanIPs, 'lan', '局域网IP'),
    wan: calculateGroupTotal(wanIPs, 'wan', '外网IP'),
    unknown: calculateGroupTotal(unknownIPs, 'unknown', '未知IP'),
  };
});

// 获取聚合统计表格中当前显示的 IP 地址
// 返回按优先级排序的IP列表（局域网IP优先）
const getAggregationVisibleIps = (): { lan: string[]; wan: string[]; unknown: string[]; allNeedsQuery: string[] } => {
  const lan: string[] = [];
  const wan: string[] = [];
  const unknown: string[] = [];
  const allNeedsQuery: string[] = [];

  // 安全检查：确保 aggregationData 已初始化
  if (!aggregationData.value) {
    return { lan, wan, unknown, allNeedsQuery };
  }

  // 按分组遍历，优先收集局域网IP
  const groups: { data: typeof aggregationData.value.lan; target: string[] }[] = [
    { data: aggregationData.value.lan, target: lan },
    { data: aggregationData.value.wan, target: wan },
    { data: aggregationData.value.unknown, target: unknown },
  ];

  for (const { data, target } of groups) {
    if (!data || !data.ips) continue;
    for (const ipStats of data.ips) {
      const ip = ipStats.ip;
      // 检查是否在全局缓存中
      const cached = getCachedHostname(ip);
      if (!cached && !dnsCache.value.has(ip)) {
        target.push(ip);
        allNeedsQuery.push(ip);
      }
    }
  }

  return { lan, wan, unknown, allNeedsQuery };
};

// 查询聚合统计 DNS
// 严格按类型分批查询：局域网IP批次中只包含局域网IP，外网IP批次中只包含外网IP
// 这样可以避免外网IP的慢查询拖累局域网IP的快速查询
const queryAggregationDns = async (forceQueryAll = false) => {
  if (!enableAggregationDns.value || aggregationQuerying.value) return;
  const { lan, wan, unknown, allNeedsQuery } = getAggregationVisibleIps();

  // 如果没有需要查询的IP，且不是强制查询全部，则直接返回
  if (allNeedsQuery.length === 0 && !forceQueryAll) return;

  aggregationQuerying.value = true;
  try {
    // 并行启动三个类型的查询，但每个类型内部是串行的
    // 这样可以保证：
    // 1. 局域网IP的查询批次中绝对不会混入外网IP
    // 2. 外网IP的查询批次中绝对不会混入局域网IP
    // 3. 三个类型并行查询，互不影响

    const queryPromises: Promise<void>[] = [];

    // 局域网IP查询任务
    if (lan.length > 0) {
      queryPromises.push(
        queryDns(lan).then(results => {
          for (const [ip, hostname] of results) {
            dnsCache.value.set(ip, hostname);
          }
        })
      );
    }

    // 外网IP查询任务（与局域网IP并行，但独立批次）
    if (wan.length > 0) {
      queryPromises.push(
        queryDns(wan).then(results => {
          for (const [ip, hostname] of results) {
            dnsCache.value.set(ip, hostname);
          }
        })
      );
    }

    // 未知IP查询任务（也与前两者并行，独立批次）
    if (unknown.length > 0) {
      queryPromises.push(
        queryDns(unknown).then(results => {
          for (const [ip, hostname] of results) {
            dnsCache.value.set(ip, hostname);
          }
        })
      );
    }

    // 等待所有类型的第一批查询完成
    await Promise.all(queryPromises);

    // 如果需要查询全部（初始加载时），对已经缓存的IP进行刷新
    // 同样严格按类型分开查询
    if (forceQueryAll) {
      // 构建每个类型的完整IP列表（用于筛选已缓存的IP）
      const allLanIps: string[] = [];
      const allWanIps: string[] = [];
      const allUnknownIps: string[] = [];

      if (aggregationData.value) {
        for (const ipStats of aggregationData.value.lan.ips) {
          allLanIps.push(ipStats.ip);
        }
        for (const ipStats of aggregationData.value.wan.ips) {
          allWanIps.push(ipStats.ip);
        }
        for (const ipStats of aggregationData.value.unknown.ips) {
          allUnknownIps.push(ipStats.ip);
        }
      }

      // 筛选出每个类型中已经缓存的IP（用于刷新）
      const remainingLan = allLanIps.filter(ip => !lan.includes(ip));
      const remainingWan = allWanIps.filter(ip => !wan.includes(ip));
      const remainingUnknown = allUnknownIps.filter(ip => !unknown.includes(ip));

      const refreshPromises: Promise<void>[] = [];

      // 刷新局域网IP（独立批次，绝不混入其他类型）
      if (remainingLan.length > 0) {
        refreshPromises.push(
          queryDns(remainingLan).then(results => {
            for (const [ip, hostname] of results) {
              dnsCache.value.set(ip, hostname);
            }
          })
        );
      }

      // 刷新外网IP（独立批次，绝不混入其他类型）
      if (remainingWan.length > 0) {
        refreshPromises.push(
          queryDns(remainingWan).then(results => {
            for (const [ip, hostname] of results) {
              dnsCache.value.set(ip, hostname);
            }
          })
        );
      }

      // 刷新未知IP（独立批次，绝不混入其他类型）
      if (remainingUnknown.length > 0) {
        refreshPromises.push(
          queryDns(remainingUnknown).then(results => {
            for (const [ip, hostname] of results) {
              dnsCache.value.set(ip, hostname);
            }
          })
        );
      }

      // 等待所有刷新查询完成
      await Promise.all(refreshPromises);
    }
  } finally {
    aggregationQuerying.value = false;
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
    // 立即执行一次查询（首次加载时使用 forceQueryAll 批量查询所有IP）
    if (aggEnabled) queryAggregationDns(true);
    if (connEnabled) queryConnectionsDns(true);
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

// 监听聚合统计数据变化，立即批量查询DNS（优先查询未缓存的IP）
watch(() => props.aggregationData, () => {
  if (enableAggregationDns.value) {
    // 使用 nextTick 确保数据已渲染，然后立即批量查询
    nextTick(() => {
      queryAggregationDns(true);
    });
  }
}, { immediate: true });

// 监听连接数据变化，立即批量查询DNS（优先查询未缓存的IP）
watch(() => props.connectionData, () => {
  if (enableConnectionsDns.value) {
    // 使用 nextTick 确保数据已渲染，然后立即批量查询
    nextTick(() => {
      queryConnectionsDns(true);
    });
  }
}, { immediate: true });

// ================= 7. 辅助函数 =================
const formatIP = (ip: string | undefined, family: string | undefined): string => {
  if (!ip) return '-';
  if (family?.toLowerCase() === 'ipv6') {
    return `[${ip}]`;
  }
  return ip;
};

// 复制功能
const copyInfo = (row: any) => {
  let source_ip: string = row.source_ip
  let destination_ip: string = row.destination_ip
  if (row.ip_family?.toLowerCase() === 'ipv6') {
    source_ip = getIpv6Display(row.source_ip);
    destination_ip = getIpv6Display(row.destination_ip);
  } else {
    source_ip = getIpDisplay(source_ip);
    destination_ip = getIpDisplay(destination_ip);
  }

  const text = `[${row.ip_family}] ${row.protocol} ${source_ip}:${row.source_port} -> ${destination_ip}:${row.destination_port} | 状态: ${row.state || '-'} | 流量: ${formatMetric(row.traffic.value, row.traffic.unit)} (${row.packets} Pkgs)`;

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
      let displayIp = ip;
      if (row.ip_family?.toLowerCase() === 'ipv6') {
        displayIp = getIpv6Display(ip);
      } else {
        displayIp = getIpDisplay(ip);
      }
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
      let displayIp = ip;
      if (row.ip_family?.toLowerCase() === 'ipv6') {
        displayIp = getIpv6Display(ip);
      } else {
        displayIp = getIpDisplay(ip);
      }
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
      return h('span', { class: 'text-slate-300' }, formatMetric(row.traffic.value, row.traffic.unit) + ' (' + row.packets + ' Pkgs.)');
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
      const formattedValue = formatMetric(trafficValue, trafficUnit);
      const fullDisplayValue = `${formattedValue} (${packets} Pkgs.)`;

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

// ================= 聚合统计分页相关配置 =================

// 获取当前激活Tab的分页状态
const currentAggregationState = computed(() => aggregationPageStates[activeAggregationTab.value]);

// 获取当前激活Tab的IP列表
const currentAggregationIps = computed(() => {
  const group = aggregationData.value[activeAggregationTab.value];
  return group?.ips || [];
});

// 计算分页后的数据
const paginatedAggregationIps = computed(() => {
  const state = currentAggregationState.value;
  const allIps = currentAggregationIps.value;
  const start = state.currentPage * state.pageSize;
  const end = start + state.pageSize;
  return allIps.slice(start, end);
});

// 计算总页数
const aggregationPageCount = computed(() => {
  const state = currentAggregationState.value;
  const total = currentAggregationIps.value.length;
  return Math.ceil(total / state.pageSize) || 1;
});

// 能否上一页
const canAggregationPreviousPage = computed(() => currentAggregationState.value.currentPage > 0);

// 能否下一页
const canAggregationNextPage = computed(() => {
  const state = currentAggregationState.value;
  return state.currentPage < aggregationPageCount.value - 1;
});

// 切换到聚合统计预设分页大小
const switchAggregationToPresetSize = async (size: number) => {
  const state = currentAggregationState.value;
  state.pageSize = size;
  state.isCustomPageSize = false;
  state.customPageSize = '';
  // 切换分页大小时保持当前页码，但确保页码有效
  const total = currentAggregationIps.value.length;
  const totalPages = Math.ceil(total / size);
  state.currentPage = totalPages > 0 ? Math.min(state.currentPage, totalPages - 1) : 0;
  state.pageInputValue = String(state.currentPage + 1);
  // 保存到数据库
  await setConfig("aggregation_table_page_size", size);
};

// 处理聚合统计自定义分页大小变更
const handleAggregationCustomPageSizeChange = async () => {
  const state = currentAggregationState.value;
  const value = parseInt(state.customPageSize, 10);
  if (!isNaN(value) && value > 0) {
    state.pageSize = value;
    state.isCustomPageSize = true;
    // 切换分页大小时保持当前页码，但确保页码有效
    const total = currentAggregationIps.value.length;
    const totalPages = Math.ceil(total / value);
    state.currentPage = totalPages > 0 ? Math.min(state.currentPage, totalPages - 1) : 0;
    state.pageInputValue = String(state.currentPage + 1);
    // 保存到数据库
    await setConfig("aggregation_table_page_size", value);
  }
};

// 聚合统计跳转到指定页
const jumpToAggregationPage = () => {
  const state = currentAggregationState.value;
  const targetPage = parseInt(state.pageInputValue, 10);
  if (isNaN(targetPage) || targetPage < 1) {
    state.pageInputValue = String(state.currentPage + 1);
    return;
  }
  const totalPages = aggregationPageCount.value;
  const validPage = Math.min(Math.max(targetPage, 1), totalPages);
  state.currentPage = validPage - 1;
  state.pageInputValue = String(validPage);
};

// 设置聚合统计页码
const setAggregationPageIndex = (index: number) => {
  const state = currentAggregationState.value;
  const totalPages = aggregationPageCount.value;
  state.currentPage = Math.min(Math.max(index, 0), totalPages - 1);
  state.pageInputValue = String(state.currentPage + 1);
};

// 聚合统计上一页
const previousAggregationPage = () => {
  if (canAggregationPreviousPage.value) {
    const state = currentAggregationState.value;
    state.currentPage--;
    state.pageInputValue = String(state.currentPage + 1);
  }
};

// 聚合统计下一页
const nextAggregationPage = () => {
  if (canAggregationNextPage.value) {
    const state = currentAggregationState.value;
    state.currentPage++;
    state.pageInputValue = String(state.currentPage + 1);
  }
};

// 切换Tab时重置页码到第一页，并保存到数据库
watch(activeAggregationTab, async (newTab) => {
  const state = currentAggregationState.value;
  state.currentPage = 0;
  state.pageInputValue = '1';
  // 保存当前选中的Tab到数据库
  await setNavState('network_connection_aggregation_active_tab', newTab);
});

// 监听聚合统计数据变化，检查页码越界
watch(() => currentAggregationIps.value, () => {
  nextTick(() => {
    const state = currentAggregationState.value;
    const totalPages = Math.max(1, aggregationPageCount.value);
    const currentIndex = state.currentPage;

    // 如果当前页码超过最大页数，跳到最后一页
    if (currentIndex >= totalPages) {
      const newIndex = totalPages - 1;
      state.currentPage = newIndex;
      state.pageInputValue = String(newIndex + 1);
    }
  });
}, { immediate: true });

// 监听聚合统计分页大小设置变化，从外部更新时同步到组件
watch(() => settings.aggregation_table_page_size, (newValue) => {
  if (newValue && newValue !== aggregationPageStates.lan.pageSize) {
    aggregationPageStates.lan.pageSize = newValue;
    aggregationPageStates.lan.isCustomPageSize = !aggregationPageSizeOptions.includes(newValue);
    aggregationPageStates.lan.customPageSize = !aggregationPageSizeOptions.includes(newValue) ? String(newValue) : '';
  }
}, { immediate: true });

watch(() => settings.aggregation_table_page_size, (newValue) => {
  if (newValue && newValue !== aggregationPageStates.wan.pageSize) {
    aggregationPageStates.wan.pageSize = newValue;
    aggregationPageStates.wan.isCustomPageSize = !aggregationPageSizeOptions.includes(newValue);
    aggregationPageStates.wan.customPageSize = !aggregationPageSizeOptions.includes(newValue) ? String(newValue) : '';
  }
}, { immediate: true });

watch(() => settings.aggregation_table_page_size, (newValue) => {
  if (newValue && newValue !== aggregationPageStates.unknown.pageSize) {
    aggregationPageStates.unknown.pageSize = newValue;
    aggregationPageStates.unknown.isCustomPageSize = !aggregationPageSizeOptions.includes(newValue);
    aggregationPageStates.unknown.customPageSize = !aggregationPageSizeOptions.includes(newValue) ? String(newValue) : '';
  }
}, { immediate: true });

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

// 排序状态
const sorting = ref<SortingState>(initialSorting);

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
    get sorting() {
      return sorting.value;
    },
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
    const formattedTraffic = formatMetric(trafficValue, trafficUnit);
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
  onSortingChange: (updater) => {
    const newSorting = typeof updater === 'function'
      ? updater(sorting.value)
      : updater;
    sorting.value = newSorting;
  },
});

// 获取排序状态的显示
const getConnectionSortIcon = (columnId: string): string => {
  if (sorting.value.length === 0 || sorting.value[0].id !== columnId) return '';
  return sorting.value[0].desc ? '↓' : '↑';
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
        <!-- DNS 查询开关 + 流量统计起始时间 + 搜索框 -->
        <div
          class="px-4 py-3 border-b border-slate-700 flex flex-col md:flex-row items-start md:items-center justify-between gap-3">
          <!-- DNS 查询开关（左侧） -->
          <label class="flex items-center gap-2 cursor-pointer shrink-0 md:w-40">
            <input type="checkbox" v-model="enableAggregationDns"
              class="w-4 h-4 rounded border-slate-600 text-blue-500 focus:ring-blue-500 bg-slate-700" />
            <span class="text-sm text-slate-300">启用 DNS 查询</span>
            <span v-if="aggregationQuerying" class="text-xs text-blue-400 animate-pulse">查询中...</span>
          </label>
          <!-- 流量统计起始时间（中间，PC端居中） -->
          <div class="flex items-center gap-2 text-xs sm:text-sm shrink-0 md:flex-1 md:justify-center">
            <span class="text-slate-400">流量统计起始时间:</span>
            <span class="text-slate-300 font-mono">{{ formatCaptureStartTime(aggregationData?.capture_start_at)
            }}</span>
          </div>
          <!-- 全局搜索框（右侧） -->
          <div class="relative w-full md:w-auto">
            <input v-model="aggregationFilter" placeholder="搜索 IP、流量、连接数..."
              class="bg-slate-900 border border-slate-600 text-white text-xs px-3 py-1.5 pr-8 rounded w-full md:w-56 min-w-32 outline-none focus:border-blue-400" />
            <button v-if="aggregationFilter" @click="aggregationFilter = ''"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300 text-xs w-4 h-4 flex items-center justify-center rounded hover:bg-slate-700 transition-colors"
              title="清空搜索">
              ×
            </button>
          </div>
        </div>

        <!-- Tab 切换导航 -->
        <div class="border-b border-slate-700">
          <nav class="flex" aria-label="Tabs">
            <button v-for="group in [aggregationData.lan, aggregationData.wan, aggregationData.unknown]"
              :key="group.key" @click="activeAggregationTab = group.key"
              class="flex-1 px-4 py-3 text-sm font-medium border-b-2 transition-colors whitespace-nowrap" :class="[
                activeAggregationTab === group.key
                  ? 'border-blue-500 text-blue-400 bg-slate-700/30'
                  : 'border-transparent text-slate-400 hover:text-slate-300 hover:bg-slate-700/20'
              ]">
              <span class="flex items-center justify-center gap-2">
                <span>{{ group.name }}</span>
                <span class="text-xs px-2 py-0.5 rounded-full bg-slate-700 text-slate-300">{{ group.ips.length }}</span>
              </span>
            </button>
          </nav>
        </div>

        <div class="overflow-x-auto">
          <table class="w-full text-sm text-center border-collapse">
            <thead class="bg-slate-700/50 text-slate-300">
              <tr>
                <th @click="toggleAggregationSort('ip')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      IP 地址
                      <span class="text-slate-400">{{ getSortIcon('ip') }}</span>
                    </div>
                  </div>
                </th>
                <th @click="toggleAggregationSort('totalThroughput')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      总实时速率
                      <span class="text-slate-400">{{ getSortIcon('totalThroughput') }}</span>
                    </div>
                    <span class="text-slate-200 font-mono font-semibold">
                      {{ formatIOBytes(aggregationData[activeAggregationTab].totalThroughput) }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('uploadThroughput')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      实时上行
                      <span class="text-slate-400">{{ getSortIcon('uploadThroughput') }}</span>
                    </div>
                    <span class="text-orange-400 font-mono font-semibold">
                      {{ formatIOBytes(aggregationData[activeAggregationTab].UploadThroughput) }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('downloadThroughput')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      实时下行
                      <span class="text-slate-400">{{ getSortIcon('downloadThroughput') }}</span>
                    </div>
                    <span class="text-cyan-400 font-mono font-semibold">
                      {{ formatIOBytes(aggregationData[activeAggregationTab].DownloadThroughput) }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('totalTraffic')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      累计上下行流量
                      <span class="text-slate-400">{{ getSortIcon('totalTraffic') }}</span>
                    </div>
                    <span class="text-slate-200 font-mono font-semibold">
                      {{ formatDataBytes(aggregationData[activeAggregationTab].totalTraffic) }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('totalUpload')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      累计上行流量
                      <span class="text-slate-400">{{ getSortIcon('totalUpload') }}</span>
                    </div>
                    <span class="text-slate-200 font-mono font-semibold">
                      {{ formatDataBytes(aggregationData[activeAggregationTab].totalUpload) }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('totalDownload')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      累计下行流量
                      <span class="text-slate-400">{{ getSortIcon('totalDownload') }}</span>
                    </div>
                    <span class="text-slate-200 font-mono font-semibold">
                      {{ formatDataBytes(aggregationData[activeAggregationTab].totalDownload) }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('tcp')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      TCP 连接
                      <span class="text-slate-400">{{ getSortIcon('tcp') }}</span>
                    </div>
                    <span class="text-blue-400 font-mono font-semibold">
                      {{ aggregationData[activeAggregationTab].totalTcp }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('udp')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      UDP 连接
                      <span class="text-slate-400">{{ getSortIcon('udp') }}</span>
                    </div>
                    <span class="text-violet-400 font-mono font-semibold">
                      {{ aggregationData[activeAggregationTab].totalUdp }}
                    </span>
                  </div>
                </th>
                <th @click="toggleAggregationSort('other')"
                  class="px-3 py-3 font-medium text-center whitespace-nowrap cursor-pointer select-none hover:text-white hover:bg-slate-700/50 transition-colors">
                  <div class="flex flex-col items-center gap-1">
                    <div class="flex items-center justify-center gap-1">
                      其他连接
                      <span class="text-slate-400">{{ getSortIcon('other') }}</span>
                    </div>
                    <span class="text-slate-200 font-mono font-semibold">
                      {{ aggregationData[activeAggregationTab].totalOther }}
                    </span>
                  </div>
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-700">
              <!-- 分页后的数据 -->
              <tr v-for="ipStats in paginatedAggregationIps" :key="ipStats.ip"
                :class="['hover:bg-slate-700/30 transition-colors', getThroughputBgClass(ipStats.totalThroughput.unit)]">
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-300" :title="ipStats.ip">{{ ipStats.ipFamily == "ipv4" ?
                    getIpDisplay(ipStats.ip) : getIpv6Display(ipStats.ip, true) }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{
                    formatMetric(ipStats.totalThroughput.value, ipStats.totalThroughput.unit) }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-orange-400">{{
                    formatMetric(ipStats.uploadThroughput.value, ipStats.uploadThroughput.unit) }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-cyan-400">{{
                    formatMetric(ipStats.downloadThroughput.value, ipStats.downloadThroughput.unit) }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-slate-200">{{
                    formatMetric(ipStats.totalTraffic.value, ipStats.totalTraffic.unit) }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-orange-400">{{
                    formatMetric(ipStats.totalUpload.value, ipStats.totalUpload.unit) }}</span>
                </td>
                <td class="px-3 py-2 text-center">
                  <span class="font-mono text-cyan-400">{{
                    formatMetric(ipStats.totalDownload.value, ipStats.totalDownload.unit) }}</span>
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
              <tr v-if="currentAggregationIps.length === 0">
                <td colspan="10" class="px-5 py-4 text-center text-slate-500 text-xs">暂无{{
                  aggregationData[activeAggregationTab].name }}数据</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- 聚合统计分页控件 -->
        <PaginationControls :pageSize="currentAggregationState.pageSize" :pageSizeOptions="aggregationPageSizeOptions"
          :isCustomPageSize="currentAggregationState.isCustomPageSize"
          v-model:customPageSize="currentAggregationState.customPageSize"
          v-model:pageInputValue="currentAggregationState.pageInputValue"
          :currentPageIndex="currentAggregationState.currentPage" :pageCount="aggregationPageCount"
          :totalRows="currentAggregationIps.length" :canPreviousPage="canAggregationPreviousPage"
          :canNextPage="canAggregationNextPage" @switchToPresetSize="switchAggregationToPresetSize"
          @handleCustomPageSizeChange="handleAggregationCustomPageSizeChange" @jumpToPage="jumpToAggregationPage"
          @setPageIndex="setAggregationPageIndex" @previousPage="previousAggregationPage"
          @nextPage="nextAggregationPage" />
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
        <div
          class="px-4 py-3 border-b border-slate-700 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
          <!-- DNS 查询开关 -->
          <label class="flex items-center gap-2 cursor-pointer shrink-0">
            <input type="checkbox" v-model="enableConnectionsDns"
              class="w-4 h-4 rounded border-slate-600 text-blue-500 focus:ring-blue-500 bg-slate-700" />
            <span class="text-sm text-slate-300">启用 DNS 查询</span>
            <span v-if="connectionsQuerying" class="text-xs text-blue-400 animate-pulse">查询中...</span>
          </label>
          <!-- 全局搜索框 -->
          <div class="relative w-full sm:w-auto">
            <input v-model="globalFilter" placeholder="全局搜索..."
              class="bg-slate-900 border border-slate-600 text-white text-xs px-3 py-1.5 pr-8 rounded w-full sm:w-56 min-w-32 outline-none focus:border-blue-400" />
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
                  <div v-if="header.column.getCanFilter()" class="mt-1 flex justify-center">
                    <div class="relative min-w-15">
                      <input :value="header.column.getFilterValue() ?? ''"
                        @input="e => header.column.setFilterValue((e.target as HTMLInputElement).value)"
                        :placeholder="`过滤 ${header.column.columnDef.header as string}...`"
                        class="bg-slate-900 border border-slate-600 text-xs px-1 py-0.5 pr-6 rounded w-full min-w-15 text-slate-200 outline-none"
                        @click.stop />

                      <button v-if="header.column.getFilterValue()" @click.stop="header.column.setFilterValue('')"
                        class="absolute right-1 top-1/2 -translate-y-1/2 text-xs w-4 h-4 flex items-center justify-center rounded text-slate-500 hover:text-slate-300 hover:bg-slate-700 transition-colors"
                        title="清空搜索">
                        ×
                      </button>
                    </div>
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

        <!-- 分页相关控件 -->
        <PaginationControls v-model:pageSize="pageSize" v-model:isCustomPageSize="isCustomPageSize"
          v-model:customPageSize="customPageSize" v-model:pageInputValue="pageInputValue"
          :pageSizeOptions="pageSizeOptions" :currentPageIndex="currentPage" :pageCount="table.getPageCount()"
          :totalRows="table.getFilteredRowModel().rows.length" :canPreviousPage="table.getCanPreviousPage()"
          :canNextPage="table.getCanNextPage()" @switchToPresetSize="switchToPresetSize"
          @handleCustomPageSizeChange="handleCustomPageSizeChange" @jumpToPage="jumpToPage"
          @setPageIndex="(index) => table.setPageIndex(index)" @previousPage="table.previousPage()"
          @nextPage="table.nextPage()" />

      </div>
    </div>
  </div>
</template>
