<template>
  <div class="min-h-screen flex flex-col sm:flex-row bg-gray-50 dark:bg-gray-950 transition-colors duration-200">
    <!-- Sidebar -->
    <aside class="w-full sm:w-64 border-r border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 shadow-sm sm:min-h-screen flex flex-col">
      <div class="p-6 border-b border-gray-200 dark:border-gray-800 flex justify-between items-center">
        <h1 class="text-xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-primary-500 to-primary-700 dark:from-primary-400 dark:to-primary-600 truncate">
          <UIcon name="i-lucide-layers" class="mr-2 translate-y-[-2px] inline-block text-primary-500"/>
          Go Ledger
        </h1>
        
        <!-- Mobile menu button -->
        <UButton 
          icon="i-lucide-menu" 
          color="neutral" 
          variant="ghost" 
          class="sm:hidden" 
          @click="isMobileMenuOpen = !isMobileMenuOpen"
        />
      </div>

      <nav :class="['flex-1 p-4 space-y-2', isMobileMenuOpen ? 'block' : 'hidden sm:block']">
        <UButton
          v-for="item in navigation"
          :key="item.to"
          :to="item.to"
          :icon="item.icon"
          :variant="route.path === item.to ? 'solid' : 'ghost'"
          :color="route.path === item.to ? 'primary' : 'neutral'"
          class="w-full justify-start font-medium"
        >
          {{ item.label }}
        </UButton>
      </nav>

      <div class="p-4 border-t border-gray-200 dark:border-gray-800 flex justify-between items-center hidden sm:flex">
        <ClientOnly>
          <UButton
            :icon="isDark ? 'i-lucide-moon' : 'i-lucide-sun'"
            color="neutral"
            variant="ghost"
            aria-label="Theme"
            @click="isDark = !isDark"
          />
          <template #fallback>
            <div class="w-8 h-8" />
          </template>
        </ClientOnly>
        
        <UButton
          v-if="token"
          icon="i-lucide-log-out"
          color="error"
          variant="ghost"
          @click="handleLogout"
          tooltip="Logout"
        />
      </div>
    </aside>

    <!-- Main Content -->
    <main class="flex-1 overflow-x-hidden flex flex-col min-h-[calc(100vh-64px)] sm:min-h-screen relative">
      <div class="p-4 sm:p-8 flex-1 w-full max-w-7xl mx-auto">
        <slot />
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'

const route = useRoute()
const router = useRouter()
const { token, logout } = useAuth()
const colorMode = useColorMode()

const isMobileMenuOpen = ref(false)

const isDark = computed({
  get() {
    return colorMode.value === 'dark'
  },
  set() {
    colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'
  }
})

const navigation = [
  { label: 'Dashboard', to: '/', icon: 'i-lucide-home' },
  { label: 'Balances', to: '/balances', icon: 'i-lucide-pie-chart' },
  { label: 'Register', to: '/transactions', icon: 'i-lucide-file-text' },
  { label: 'Postings', to: '/postings', icon: 'i-lucide-users' },
  { label: 'New Transaction', to: '/transactions/new', icon: 'i-lucide-circle-plus' }
]

const handleLogout = () => {
  logout()
  router.push('/login')
}
</script>
