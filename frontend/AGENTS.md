# AGENTS.md — OpenWrt Monitor Frontend

此项目是一个Vue 3 SPA前端项目，用于展示和查询 OpenWrt 系统指标（CPU/内存/网络/磁盘IO），含 PWA。

## 构建 & 开发

```bash
pnpm dev
pnpm build
pnpm preview
```

## 运行检查

```bash
pnpm vue-tsc --noEmit   # 类型检查
```

## 技术栈 & 代码风格

- **Vue 3** Composition API
- **TypeScript**：`tsconfig.json`
- **Tailwind CSS v4**：`@import "tailwindcss"` 语法（非 v3 的 `@tailwind` 指令）

### 部分重要依赖说明

| 包                        | 用途                                            |
| ------------------------- | ----------------------------------------------- |
| `vue-echarts` + `echarts` | MonitoringCharts 图表                           |
| `@tanstack/vue-table`     | NetworkConnectionTable 表格                     |
| `dexie`                   | 浏览器 IndexedDB，`src/utils/db.ts` 定义 schema |
| `dayjs`                   | 时间格式化                                      |
| `vite-plugin-pwa`         | PWA manifest / service worker                   |
