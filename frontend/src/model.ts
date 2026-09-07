// 基础指标结构
export interface Metric {
  value: number;
  unit: string;
}

// 动态数据结构
export interface StorageData {
  read: Metric;
  write: Metric;
  total: Metric;
  used: Metric;
  used_percent: Metric;
}

export interface CpuData {
  usage: Metric;
  temperature: Metric;
}

export interface NetworkDynamicData {
  incoming: Metric;
  outgoing: Metric;
}

export interface SystemDynamicData {
  uptime: string;
}

export interface DynamicApiResponse {
  storage?: { [key: string]: StorageData };
  cpu?: { [key: string]: CpuData }; // key 是 cpu0, cpu1...
  network?: { total: NetworkDynamicData; [key: string]: NetworkDynamicData };
  memory?: {
    total: Metric;
    used: Metric;
    used_percent: Metric;
  };
  system?: SystemDynamicData;
}

// 静态数据结构
export interface IpConfig {
  ipv4: string[];
  ipv6: string[];
}

export interface NetworkStaticData {
  [key: string]: IpConfig & { gateway?: string; dns?: string[] };
}

export interface SystemStaticData {
  hostname: string;
  kernel: string;
  os: string;
  arch: string;
  timezone: string;
  device_name: string;
}

export interface StaticApiResponse {
  network?: NetworkStaticData;
  system?: SystemStaticData;
}

// 连接数据结构
export interface Connection {
  ip_family: string;
  source_ip: string;
  source_port: number;
  destination_ip: string;
  destination_port: number;
  protocol: string;
  state: string;
  traffic: Metric;
  packets: number;
}

export interface ConnectionApiResponse {
  counts?: { tcp: number; udp: number; other: number };
  connections?: Connection[];
}

// ================= 历史数据结构 =================
export interface HistoryRecord {
  id?: number; // Dexie 自增 ID
  timestamp: number; // 时间戳
  metric:
    | "cpu_total"
    | "cpu_temp"
    | "memory_total"
    | "memory_used"
    | "memory_used_percent"
    | "network_in"
    | "network_out"
    | "storage_total_io"
    | "storage_usage"
    | "connections"
    | "storage_space";
  value: number; // 数值
  unit: string; // 单位
  label?: string; // 子标签，用于区分多条折线，例如 'cpu0', 'eth0-in', 'tcp' 等
}

// ================= 用户配置结构 =================
export interface UserSetting {
  key: string; // 键名，例如 'retention_days', 'theme'
  value: string | number | boolean; // 值
}

// ================= 聚合流量统计结构 =================
export type IpAddressType = "lan" | "wan" | "unknown";
export const IpAddressTypeList = ["lan", "wan", "unknown"];
export type IpFamilyType = "ipv4" | "ipv6";

export interface MetricUnit {
  value: number;
  unit: string;
}

export interface AggregationTrafficDetails {
  ip: string;
  ip_type: IpAddressType;
  ip_family: IpFamilyType;
  incoming: MetricUnit;
  outgoing: MetricUnit;
  total_throughput: MetricUnit;
  total_incoming: MetricUnit;
  total_outgoing: MetricUnit;
  total_traffic: MetricUnit;
  upload_pkts: MetricUnit;
  download_pkts: MetricUnit;
  total_pkts: MetricUnit;
  tcp: number;
  udp: number;
  other: number;
}

export interface AggregationTrafficMetric {
  capture_start_at: string;
  capture_interface: string;
  details: AggregationTrafficDetails[];
}

export type AggregationTrafficResponse = AggregationTrafficMetric;

export const TimeRanges = [
  { label: "1 分钟", value: 60 * 1000 },
  { label: "10 分钟", value: 10 * 60 * 1000 },
  { label: "30 分钟", value: 30 * 60 * 1000 },
  { label: "1 小时", value: 60 * 60 * 1000 },
  { label: "6 小时", value: 6 * 60 * 60 * 1000 },
  { label: "12 小时", value: 12 * 60 * 60 * 1000 },
  { label: "1 天", value: 24 * 60 * 60 * 1000 },
  { label: "3 天", value: 3 * 24 * 60 * 60 * 1000 },
  { label: "7 天", value: 7 * 24 * 60 * 60 * 1000 },
  { label: "1 月", value: 30 * 24 * 60 * 60 * 1000 },
  { label: "2 月", value: 60 * 24 * 60 * 60 * 1000 },
  { label: "3 月", value: 90 * 24 * 60 * 60 * 1000 },
  { label: "6 月", value: 180 * 24 * 60 * 60 * 1000 },
  { label: "1 年", value: 365 * 24 * 60 * 60 * 1000 },
];
