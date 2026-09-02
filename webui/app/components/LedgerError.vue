<script setup lang="ts">
defineProps<{
  /** The failure to report. */
  error: Error | null
  /** What could not be read, e.g. 'Failed to load the trial balance'. */
  title: string
}>()

defineEmits<{ retry: [] }>()
</script>

<template>
  <div
    class="bg-red-50 dark:bg-red-900/20 p-6 rounded-xl border border-red-200 dark:border-red-800 text-center"
  >
    <UIcon
      name="i-lucide-triangle-alert"
      class="text-4xl text-red-500 mb-2"
    />
    <h3 class="text-lg font-medium text-red-800 dark:text-red-400">
      {{ title }}
    </h3>
    <p class="text-red-600 dark:text-red-300 mt-1">
      {{ error?.message || 'Please check your connection and authentication.' }}
    </p>
    <UButton
      color="error"
      variant="soft"
      class="mt-4"
      @click="$emit('retry')"
    >
      Retry
    </UButton>
  </div>
</template>
