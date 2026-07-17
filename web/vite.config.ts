import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 6173,
    proxy: {
      '/v1': {
        target: 'http://localhost:9080',
        changeOrigin: true,
      },
    },
  },
})
