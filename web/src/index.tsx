// admin UI 插件入口（PluginFrontendModule export）
//
// 健康监控插件目前不再注册任何 admin 前端页面：
//   - 分组级可用率直接由 core 的分组管理页展示（数据通过 admin API 拉取或后续
//     用 SDK FrontendWidget 槽机制内嵌）
//   - 本插件只保留后端 prober + group_health_probes 表 + /admin/* JSON API 作为数据源
//
// 入口文件保留是因为 core 的 plugin-loader 仍会尝试动态 import；返回空 routes 即可。
// 公开状态页（/status）走独立的 StatusPage.tsx 入口，不在这里。

import type { ComponentType } from 'react';

interface PluginFrontendModule {
  routes?: Array<{ path: string; component: ComponentType }>;
}

const plugin: PluginFrontendModule = {
  routes: [],
};

export default plugin;
