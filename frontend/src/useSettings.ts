import { reactive, readonly } from 'vue';
import { db } from './utils/db';

export type TabType = 'system' | 'network' | 'monitoringCharts';

export interface Settings {
  enable_metric_record: boolean;
  retention_days: number;
  refresh_interval: number;
  active_tab: TabType;
  chart_time_range: number;
  network_table_page_size: number;
}

export const defaultSettings: Settings = {
  enable_metric_record: false,
  retention_days: 7,
  refresh_interval: 2000,
  active_tab: 'system',
  chart_time_range: 60 * 1000,
  network_table_page_size: 20
};

const settings = reactive<Settings>({ ...defaultSettings });

let initialized = false;
const initPromise = (async () => {
  const keys = Object.keys(defaultSettings) as (keyof Settings)[];
  await Promise.all(
    keys.map(async (key) => {
      const record = await db.settings.get(key);
      if (record?.value !== undefined) {
        (settings as any)[key] = record.value;
      }
    })
  );
  initialized = true;
})();

export function useSettings() {
  const setConfig = async <K extends keyof Settings>(key: K, value: Settings[K]) => {
    await db.settings.put({ key, value });
    (settings as any)[key] = value;
  };

  const init = () => initPromise;

  const isInitialized = () => initialized;

  return {
    settings: readonly(settings),
    setConfig,
    init,
    isInitialized
  };
}
