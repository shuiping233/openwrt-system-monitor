import { UnitType } from "dayjs";
import { number } from "echarts";
import { numCalculate } from "echarts/types/src/component/marker/markerHelper.js";

export function convertToBytes(value: number, unit: string): number {
  const unitMultipliers: { [key: string]: number } = {
    'B': 1,
    'KB': 1024,
    'MB': 1024 * 1024,
    'GB': 1024 * 1024 * 1024,
    'TB': 1024 * 1024 * 1024 * 1024,
    'PB': 1024 * 1024 * 1024 * 1024 * 1024,
  };

  const multiplier = unitMultipliers[unit.toUpperCase()] || 1;
  return value * multiplier;
}

export function BytesFixed(bytes: number, unit: string): string {
  if (bytes < 0) {
    return "-1";
  }
  if (unit === 'B' || unit === 'B/S' || unit === '%') {
    return bytes.toFixed(0);
  }
  return bytes.toFixed(2);
};

export const RATE_UNITS = ['B/S', 'KB/S', 'MB/S', 'GB/S', 'TB/S', 'PB/S'] as const;
type RateUnit = typeof RATE_UNITS[number];

export const DATA_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const;
type DataUnit = typeof DATA_UNITS[number];

export function trimUnit(unit: string): RateUnit | DataUnit | string {
  const u = unit.trim().replace(/\/s$/i, '/S');
  if ((RATE_UNITS as readonly string[]).includes(u)) return u as RateUnit;
  if ((DATA_UNITS as readonly string[]).includes(u)) return u as DataUnit;
  return u;
}

// 2. 归一化 → Bytes/s（未知单位原样返回 value）
export function normalizeToBytes(value: number, unit: string): number {
  const u = trimUnit(unit);

  // 只处理已知单位
  const list: readonly string[] = RATE_UNITS.includes(u as RateUnit)
    ? RATE_UNITS
    : DATA_UNITS;
  const idx = list.indexOf(u);
  if (idx === -1) return value; // 未知单位，原样返回

  // 1024 的 idx 次方
  const scale = Math.pow(1024, idx);
  return value * scale;
}

// 3. 格式化 ← Bytes/s（递归版，固定输出 RateUnit）
export function formatIOBytes(bytes: number, idx = 0): string {
  if (bytes === 0) {
    return `0 ${RATE_UNITS[0]}`;
  }
  if (idx >= RATE_UNITS.length - 1) {
    // 已最大单位
    return `${(bytes / Math.pow(1024, idx)).toFixed(2)} ${RATE_UNITS[idx]}`;
  }
  if (bytes < Math.pow(1024, idx + 1)) {
    // 适合当前单位
    return `${(bytes / Math.pow(1024, idx)).toFixed(2)} ${RATE_UNITS[idx]}`;
  }
  return formatIOBytes(bytes, idx + 1); // 继续往大单位走
}

// 3. 格式化 ← Bytes（递归版，固定输出 DataUnit）
export function formatDataBytes(bytes: number, idx = 0): string {
  if (bytes === 0) {
    return `0 ${DATA_UNITS[0]}`;
  }
  if (idx >= DATA_UNITS.length - 1) {
    // 已最大单位
    return `${(bytes / Math.pow(1024, idx)).toFixed(2)} ${DATA_UNITS[idx]}`;
  }
  if (bytes < Math.pow(1024, idx + 1)) {
    // 适合当前单位
    return `${(bytes / Math.pow(1024, idx)).toFixed(2)} ${DATA_UNITS[idx]}`;
  }
  return formatDataBytes(bytes, idx + 1); // 继续往大单位走
}

// 转换DATA_UNITS单位位目标单位
export function covertDataBytes(bytes: number, unit: string, target: string): [number, string] {
  if (bytes === 0) {
    return [0, DATA_UNITS[0]];
  }
  const idx = DATA_UNITS.indexOf(unit as DataUnit);
  const targetIdx = DATA_UNITS.indexOf(target as DataUnit);
  if (targetIdx === -1) return [bytes, unit];
  if (idx === -1) return [bytes, unit];

  if (unit as DataUnit === target) {
    return [bytes, unit];
  }

  if (idx >= DATA_UNITS.length - 1) {
    // 已最大单位
    return [bytes, DATA_UNITS[idx]];
  }

  const diff = idx - targetIdx;
  if (diff < 0) {
    // 往小单位走
    bytes = bytes / Math.pow(1024, -diff);
  }
  if (diff > 0) {
    // 往大单位走
    bytes = bytes * Math.pow(1024, diff);
  }
  return [bytes, DATA_UNITS[targetIdx]];
}
