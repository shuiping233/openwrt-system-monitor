# AGENTS.md — OpenWrt Monitor Frontend

此项目是一个Vue 3 SPA前端项目，用于展示和查询 OpenWrt 系统指标（CPU/内存/网络/磁盘IO），含 PWA。

> [!IMPORTANT]
> 目前项目还未进入正式版，仍需添加众多功能和修改不成熟的设计，所以允许大批量代码重构且不考虑接口兼容性，优先以最优性能和维护性的方式进行迭代。

> [!IMPORTANT]
> 在遇到需求时，如果用户提供的需求不够详细,且与目前最佳的性能和维护性代码实践差距过大且缺少必要关键的预期，必须立刻先反问用户，确定好方案和预期效果后，才进行开发，绝对不允许需求和预期模糊的情况下强制进行项目迭代。

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

## 后端接口数据说明

- 后端的接口格式,凡是涉及到具体数值指标的,都会带有类似`{"value": 123.34 "unit": "KB/S"}`的格式,单位的字符串值都是大写,所以要在页面中直接展示指标数据时,对`value`进行取小数点位数后,直接展示`value``unit`字段值即可,具体的接口数据类型请参考`src/model.ts`的数据类定义
