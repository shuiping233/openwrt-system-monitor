import Dexie, { Table } from "dexie";
import type { HistoryRecord, UserSetting } from "../model";

class SystemMonitorDB extends Dexie {
  history!: Table<HistoryRecord, number>;
  settings!: Table<UserSetting, string>;

  constructor() {
    super("SystemMonitorDB");
    // 定义表结构
    // 1. history: 主键自增，索引 timestamp (用于时间范围查询), 索引 metric (用于分类查询)
    this.version(1).stores({
      history: "++id, timestamp, metric",
      settings: "key", // settings 表以 key 为主键
    });
  }
}

export const db = new SystemMonitorDB();
