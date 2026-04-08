// admin UI 插件入口（PluginFrontendModule export）
//
// core 的 plugin-loader 会动态 import 这个文件的 default export，
// 找到 routes 数组中匹配 path 的 component 渲染。
// path 必须与后端 metadata.go 中 FrontendPages 声明的 path 完全一致。
//
// react / react-dom 由 core 通过 window.__airgate_shared 提供，
// vite.config.ts 已把它们标为 external，最终产物里只有插件自己的代码。

import HealthDashboard from './admin/HealthDashboard';
import type { ComponentType } from 'react';

interface PluginFrontendModule {
  routes?: Array<{ path: string; component: ComponentType }>;
}

const plugin: PluginFrontendModule = {
  routes: [
    { path: '/admin/health', component: HealthDashboard },
  ],
};

export default plugin;
