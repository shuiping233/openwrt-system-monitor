import { computed } from "vue";
import { db } from "./utils/db";
import type { HistoryRecord, UserSetting } from "./model";

export function useDatabase() {
  // ================= 配置 =================

  /**
   * 获取配置项
   */
  const getConfig = async <T = any>(key: string): Promise<T | undefined> => {
    const setting = await db.settings.get(key);
    return setting?.value as T;
  };

  /**
   * 设置/更新配置项
   */
  const setConfig = async (key: string, value: any) => {
    await db.settings.put({ key, value });
  };

  /**
   * 删除配置项
   */
  const deleteConfig = async (key: string) => {
    await db.settings.delete(key);
  };

  // ================= UI 状态 =================
  /**
   * 获取折叠面板状态
   */
  const getAccordionState = async (key: string): Promise<boolean> => {
    const state = await db.settings.get("accordion_" + key);
    return state ? state.value === true : true; // 默认展开为 true
  };

  /**
   * 设置折叠面板状态
   */
  const setAccordionState = async (key: string, isOpen: boolean) => {
    await db.settings.put({ key: "accordion_" + key, value: isOpen });
  };

  const getNavState = async (key: string): Promise<string> => {
    const state = await db.settings.get("nav_" + key);
    return state?.value as string;
  };

  /**
   * 设置折叠面板状态
   */
  const setNavState = async (key: string, value: string) => {
    await db.settings.put({ key: "nav_" + key, value: value });
  };

  // ================= 历史数据 =================

  /**
   * 添加一条历史记录
   * 内部自动清理旧数据
   */
  const addHistory = async (record: Omit<HistoryRecord, "id">) => {
    await db.history.add(record);
    await cleanOldHistory(record.metric);
  };

  /**
   * 批量添加历史记录 (用于一次拉取多指标)
   */
  const addHistoryBatch = async (records: Omit<HistoryRecord, "id">[]) => {
    if (records.length === 0) return;
    await db.history.bulkAdd(records);
    // 清理涉及的指标类型
    const metrics = [...new Set(records.map((r) => r.metric))];
    for (const m of metrics) {
      await cleanOldHistory(m);
    }
  };

  /**
   * 查询历史数据
   * @param metric 指标类型，不传则查全部
   * @param timeRange 时间范围，默认查最近24小时(毫秒)
   */
  const getHistory = async (
    metric?: HistoryRecord["metric"],
    timeRange: number = 24 * 60 * 60 * 1000,
  ): Promise<HistoryRecord[]> => {
    const endTime = Date.now();
    const startTime = endTime - timeRange;

    if (metric) {
      return await db.history
        .where("metric")
        .equals(metric)
        .and((item) => item.timestamp >= startTime)
        .sortBy("timestamp");
    } else {
      return await db.history.where("timestamp").between(startTime, endTime).sortBy("timestamp");
    }
  };

  /**
   * 清空特定指标的历史数据
   */
  const clearHistory = async (metric?: HistoryRecord["metric"]) => {
    if (metric) {
      await db.history.where("metric").equals(metric).delete();
    } else {
      await db.history.clear();
    }
  };

  // ================= 内部辅助函数 =================

  /**
   * 清理旧数据 (根据 retention_days 配置)
   */
  const cleanOldHistory = async (metric: string) => {
    const retentionDays = (await getConfig<number>("retention_days")) || 7;
    const cutoffTime = Date.now() - retentionDays * 24 * 60 * 60 * 1000;

    const count = await db.history
      .where("metric")
      .equals(metric)
      .and((item) => item.timestamp < cutoffTime)
      .delete();

    if (count > 0) {
      console.log(`[DB] Cleaned ${count} old records for ${metric}`);
    }
  };

  return {
    // Config
    getConfig,
    setConfig,
    deleteConfig,
    // UI State
    getAccordionState,
    setAccordionState,
    getNavState,
    setNavState,
    // History
    addHistory,
    addHistoryBatch,
    getHistory,
    clearHistory,
  };
}
