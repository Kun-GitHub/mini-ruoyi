import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'

// 后端直接从磁盘托管前端产物，重新构建后旧的哈希文件名会被清理。
// 已打开的页面若再做懒加载就会拿到 404，这里直接整页刷新去取最新版本。
window.addEventListener('vite:preloadError', () => location.reload())

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
