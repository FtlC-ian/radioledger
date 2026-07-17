// Configuration for your app
// https://v2.quasar.dev/quasar-cli-webpack/quasar-config-file

import { defineConfig } from '#q-app/wrappers'

export default defineConfig(() => {
  return {
    eslint: {
      warnings: false,
      errors: false,
    },

    boot: ['pinia', 'auth'],

    css: ['app.scss'],

    extras: ['roboto-font', 'material-icons'],

    build: {
      vueRouterMode: 'hash',
      env: {
        RADIOLEDGER_API_BASE_URL: process.env.RADIOLEDGER_API_BASE_URL ?? '',
        API_BASE_URL: process.env.API_BASE_URL ?? '',
        RADIOLEDGER_USER_ID: process.env.RADIOLEDGER_USER_ID ?? '',
        VUE_APP_RADIOLEDGER_USER_ID: process.env.VUE_APP_RADIOLEDGER_USER_ID ?? '',
        OIDC_AUTHORITY: process.env.OIDC_AUTHORITY ?? '',
        OIDC_CLIENT_ID: process.env.OIDC_CLIENT_ID ?? '',
        OIDC_REDIRECT_URI: process.env.OIDC_REDIRECT_URI ?? '',
      },
      esbuildTarget: {
        browser: ['es2022', 'firefox115', 'chrome115', 'safari14'],
        node: 'node20',
      },
    },

    devServer: {
      server: {
        type: 'http',
      },
      open: false,
      proxy: [
        {
          context: ['/v1', '/health', '/ready'],
          target: 'http://localhost:9091',
          changeOrigin: true,
        },
      ],
    },

    framework: {
      config: {
        dark: true,
      },
      plugins: ['Notify', 'Dialog', 'LoadingBar'],
    },

    animations: [],
  }
})
