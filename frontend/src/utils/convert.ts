export const RATE_UNITS = [
  "B/S",
  "KB/S",
  "MB/S",
  "GB/S",
  "TB/S",
  "PB/S",
] as const;
type RateUnit = (typeof RATE_UNITS)[number];

export const DATA_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"] as const;
type DataUnit = (typeof DATA_UNITS)[number];

export function convertToBytes(value: number, unit: string): number {
  const idx = DATA_UNITS.indexOf(unit.trim().toUpperCase() as DataUnit);
  if (idx === -1) return value;
  return value * Math.pow(1024, idx);
}

export function formatMetric(
  value: number,
  unit: string,
  decimals?: number,
): string {
  return `${formatValue(value, unit, decimals)} ${unit}`;
}

export function formatValue(
  value: number,
  unit: string,
  decimals?: number,
): string {
  if (value <= 0) return `${value}`;
  const dec = decimals ?? getDefaultDecimals(unit);
  return value.toFixed(dec);
}

function getDefaultDecimals(unit: string): number {
  const u = unit.trim().toUpperCase();
  if (u === "°C" || u === "°F") return 0;
  return 2;
}

export function trimUnit(unit: string): RateUnit | DataUnit | string {
  const u = unit.trim().replace(/\/s$/i, "/S");
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

function formatBytes(bytes: number, units: readonly string[], idx = 0): string {
  if (bytes === 0) return `0 ${units[0]}`;
  if (idx >= units.length - 1 || bytes < Math.pow(1024, idx + 1)) {
    return `${(bytes / Math.pow(1024, idx)).toFixed(2)} ${units[idx]}`;
  }
  return formatBytes(bytes, units, idx + 1);
}

export const formatIOBytes = (bytes: number) => formatBytes(bytes, RATE_UNITS);
export const formatDataBytes = (bytes: number) =>
  formatBytes(bytes, DATA_UNITS);

// 转换DATA_UNITS单位位目标单位
export function covertDataBytes(
  bytes: number,
  unit: string,
  target: string,
): [number, string] {
  if (bytes === 0) return [0, target];
  const idx = DATA_UNITS.indexOf(unit as DataUnit);
  const targetIdx = DATA_UNITS.indexOf(target as DataUnit);
  if (targetIdx === -1) return [bytes, unit];
  if (idx === -1) return [bytes, unit];

  if ((unit as DataUnit) === target) return [bytes, unit];

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
