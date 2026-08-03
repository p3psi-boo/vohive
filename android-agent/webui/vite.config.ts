import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

// 产物直接输出到 APK assets，由 LocalHttpServer 托管。
// 注意：PWA 安装与 Service Worker 需要安全上下文（HTTPS 或 localhost）。
// 局域网 http://<ip>:8765 不是安全上下文，浏览器会静默跳过 SW 注册，
// 页面降级为普通网页，功能不受影响。
export default defineConfig({
  base: './',
  plugins: [
    vue(),
    VitePWA({
      registerType: 'autoUpdate',
      injectRegister: 'script',
      manifest: {
        name: 'VoHive Agent',
        short_name: 'VoHive Agent',
        description: '设备连接与恢复',
        theme_color: '#FAF9F7',
        background_color: '#FAF9F7',
        display: 'standalone',
        start_url: './',
        icons: [
          { src: 'icons/icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'icons/icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'icons/maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' }
        ]
      },
      workbox: {
        // API 一律走网络，只缓存应用外壳
        runtimeCaching: [
          {
            urlPattern: /\/api\//,
            handler: 'NetworkOnly'
          }
        ],
        navigateFallback: 'index.html'
      }
    })
  ],
  build: {
    outDir: '../app/src/main/assets/web',
    emptyOutDir: true
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8765'
    }
  }
})
