// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  modules: [
    '@nuxt/eslint',
    '@nuxt/ui'
  ],

  devtools: {
    enabled: true
  },

  css: ['~/assets/css/main.css'],

  icon: {
    provider: 'server',
    localApiEndpoint: '/_nuxt_icon',
    serverBundle: {
      collections: ['lucide', 'heroicons', 'simple-icons']
    }
  },

  routeRules: {
    '/': { prerender: true }
  },

  compatibilityDate: '2025-01-15',

  eslint: {
    config: {
      stylistic: {
        commaDangle: 'never',
        braceStyle: '1tbs'
      }
    }
  },

  nitro: {
    routeRules: {
      '/api/**': { proxy: 'http://127.0.0.1:8080/**' }
    }
  }
})
