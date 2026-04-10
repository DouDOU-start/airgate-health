import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// public 状态页的独立打包：产出一个自包含的 status.html + assets/*。
// 与 admin 的 lib 模式不同——这里要把 React 一起打进去，因为公开页面没有 core 的运行时共享。
//
// 输出位于 dist/ 下，最终会被 Makefile 同步到 backend/internal/health/webdist/，
// 再由插件的 readAsset() 在 GET /status 时返回。

const watchOptions = process.argv.includes('--watch')
  ? { chokidar: { usePolling: true, interval: 1000 } }
  : undefined;

export default defineConfig({
  plugins: [react()],
  // base = /status/ 让构建出的 status.html 里所有 <script>/<link> 都用 /status/assets/...
  // 这样：浏览器 GET /status/assets/foo.js → core 的 /status/*path 反向代理路由命中 →
  // 转发到 airgate-health 的 handlePublicAsset → 从 webdist/assets/foo.js 返回。
  // 如果 base 是默认的 /，则会变成 /assets/foo.js，与 core 自己的 /assets 静态路由冲突。
  base: '/status/',
  build: {
    outDir: 'dist',
    emptyOutDir: false, // 不清掉主 build 的产物
    watch: watchOptions,
    rollupOptions: {
      input: {
        status: 'status.html',
      },
      output: {
        entryFileNames: 'assets/status-[hash].js',
        chunkFileNames: 'assets/status-[hash].js',
        assetFileNames: 'assets/status-[hash][extname]',
      },
    },
  },
});
