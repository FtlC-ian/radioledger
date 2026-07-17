import { defineConfig } from 'vitest/config'
import { resolve } from 'node:path'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
    environmentOptions: {},
  },
  resolve: {
    alias: {
      src: resolve(__dirname, 'src'),
    },
  },
})
