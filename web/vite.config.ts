import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 主 build：admin UI 模块入口，由 core 的 plugin-loader 在运行时动态 import。
// react / react-dom 由 core 通过 window.__airgate_shared 提供，插件不重复打包。
//
// public 状态页通过 vite.status.config.ts 单独构建为独立 HTML（自带 React），
// 因为它需要无登录访问，不依赖 core 的运行时共享。
export default defineConfig({
  plugins: [react()],
  build: {
    lib: {
      entry: 'src/index.tsx',
      formats: ['es'],
      fileName: 'index',
    },
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      external: ['react', 'react-dom', 'react/jsx-runtime'],
    },
  },
});
