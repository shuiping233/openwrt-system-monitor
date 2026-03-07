// 应用基础配置 - 修改这里会同时影响网页标题和PWA
export const APP_CONFIG = {
  // 应用名称（显示在浏览器标签页和PWA主标题）
  name: "OpenWrt System Monitor",

  // 短名称（PWA图标下显示，建议不超过12个字符）
  shortName: "Monitor",

  // 完整标题（HTML title）
  title: "OpenWrt 监控仪表盘",

  // PWA描述
  description: "实时监控 OpenWrt 系统资源、网络流量和磁盘I/O性能",

  // 主题色
  themeColor: "#0f172a",

  // 背景色
  backgroundColor: "#0f172a",
} as const;

// 导出类型
export type AppConfig = typeof APP_CONFIG;
