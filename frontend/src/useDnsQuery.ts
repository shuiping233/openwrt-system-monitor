import { ref, computed } from "vue";
import { useSettings } from "./useSettings";

interface CacheEntry {
  hostname: string;
  timestamp: number;
}

// 简单的 LRU 缓存实现
class SimpleLRUCache {
  private cache = new Map<string, CacheEntry>();
  private maxSize: number;
  private ttl: number; // 毫秒

  constructor(maxSize: number, ttlMinutes: number) {
    this.maxSize = maxSize;
    this.ttl = ttlMinutes * 60 * 1000;
  }

  get(key: string): string | undefined {
    const entry = this.cache.get(key);
    if (!entry) return undefined;

    // 检查是否过期
    if (Date.now() - entry.timestamp > this.ttl) {
      this.cache.delete(key);
      return undefined;
    }

    // 更新访问顺序（LRU）
    this.cache.delete(key);
    this.cache.set(key, entry);
    return entry.hostname;
  }

  set(key: string, value: string): void {
    // 如果已存在，先删除以更新顺序
    if (this.cache.has(key)) {
      this.cache.delete(key);
    }

    // 如果超过最大大小，删除最旧的条目
    if (this.cache.size >= this.maxSize) {
      const firstKey = this.cache.keys().next().value;
      if (firstKey) {
        this.cache.delete(firstKey);
      }
    }

    this.cache.set(key, {
      hostname: value,
      timestamp: Date.now(),
    });
  }

  clear(): void {
    this.cache.clear();
  }

  updateTTL(ttlMinutes: number): void {
    this.ttl = ttlMinutes * 60 * 1000;
  }
}

// 全局 DNS 缓存实例
let dnsCache: SimpleLRUCache | null = null;

export function useDnsQuery() {
  const { settings } = useSettings();
  const isQuerying = ref(false);
  const error = ref<string | null>(null);

  // 初始化或获取缓存实例
  const getCache = () => {
    if (!dnsCache) {
      dnsCache = new SimpleLRUCache(1000, settings.dns_cache_ttl);
    } else {
      // 更新 TTL 配置
      dnsCache.updateTTL(settings.dns_cache_ttl);
    }
    return dnsCache;
  };

  // 批量查询 DNS
  const queryDnsBatch = async (ips: string[]): Promise<Map<string, string>> => {
    const cache = getCache();
    const result = new Map<string, string>();
    const ipsToQuery: string[] = [];

    // 先查缓存
    for (const ip of ips) {
      const cached = cache.get(ip);
      if (cached) {
        result.set(ip, cached);
      } else {
        ipsToQuery.push(ip);
      }
    }

    // 如果都在缓存中，直接返回
    if (ipsToQuery.length === 0) {
      return result;
    }

    // 构建查询参数
    const params = new URLSearchParams();
    for (const ip of ipsToQuery) {
      params.append("ip", ip);
    }

    try {
      const response = await fetch(`/dns/query?${params.toString()}`);
      if (!response.ok) {
        throw new Error(`DNS query failed: ${response.status}`);
      }

      const data: Record<string, string[]> = await response.json();

      // 处理返回结果
      for (const ip of ipsToQuery) {
        const hostnames = data[ip];
        if (hostnames && hostnames.length > 0) {
          const hostname = hostnames[0]; // 使用第一个主机名
          cache.set(ip, hostname);
          result.set(ip, hostname);
        } else {
          // 查不到结果，记录为 IP 自己，避免重复查询
          // IPv6 地址需要压缩以保持与 key 一致
          const valueToCache = ip.includes(":") ? `${ip}` : ip;
          cache.set(ip, valueToCache);
          // 不加入 result，保持显示原 IP
        }
      }
    } catch (err) {
      console.error("DNS query error:", err);
      error.value = err instanceof Error ? err.message : "DNS query failed";
      // 查询失败不影响已有缓存结果
    }

    return result;
  };

  // 批量查询（分批处理）
  const queryDns = async (ips: string[]): Promise<Map<string, string>> => {
    if (ips.length === 0) return new Map();

    isQuerying.value = true;
    error.value = null;

    const batchSize = settings.dns_batch_size;
    const result = new Map<string, string>();

    try {
      // 分批查询
      for (let i = 0; i < ips.length; i += batchSize) {
        const batch = ips.slice(i, i + batchSize);
        const batchResult = await queryDnsBatch(batch);
        for (const [ip, hostname] of batchResult) {
          result.set(ip, hostname);
        }
      }
    } finally {
      isQuerying.value = false;
    }

    return result;
  };

  // 清除缓存
  const clearCache = () => {
    getCache().clear();
  };

  // 从缓存获取指定 IP 的 hostname（如果不存在返回 undefined）
  const getCachedHostname = (ip: string): string | undefined => {
    return getCache().get(ip);
  };

  return {
    queryDns,
    clearCache,
    getCachedHostname,
    isQuerying: computed(() => isQuerying.value),
    error: computed(() => error.value),
  };
}
