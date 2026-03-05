<template>
  <UApp>
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
  </UApp>
</template>

<script setup lang="ts">
const colorMode = useColorMode()

useHead({
  bodyAttrs: {
    class: 'antialiased bg-gray-50 dark:bg-gray-950 text-gray-900 dark:text-gray-100 transition-colors duration-200',
    style: "font-family: 'Inter', sans-serif;"
  }
})

onMounted(() => {
  // Respect system preference or default to dark mode for a modern look
  if (colorMode.preference === 'system') {
    colorMode.preference = 'dark'
  }
})
</script>
