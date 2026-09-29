import path from 'node:path'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  resolve: {
    alias: {
      $lib: path.resolve('./src/lib'),
    },
  },
  build: {
    // 产物由后端从磁盘目录托管（见 server/internal/httpserver/static.go），
    // 因此资源必须落在 assets/ 下，与后端注册的 /assets/ 前缀对齐
    assetsDir: 'assets',
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    // 开发时把后端接口代理给本地 Go 服务，前端热更新与后端互不干扰
    proxy: {
      '/api': 'http://192.168.201.66:8080',
      '/healthz': 'http://192.168.201.66:8080',
    },
  },
})
