<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <div>
        <h1 class="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
          Register
        </h1>
        <p class="text-gray-500 mt-1">
          One account's postings over time, each with its running balance.
        </p>
      </div>
      <div class="flex gap-3">
        <UButton
          color="neutral"
          variant="soft"
          icon="i-lucide-filter"
          @click="isFiltersOpen = !isFiltersOpen"
        >
          {{ isFiltersOpen ? 'Hide Filters' : 'Filter' }}
        </UButton>
        <UButton
          color="neutral"
          variant="soft"
          icon="i-lucide-refresh-cw"
          :loading="loading"
          @click="refresh"
        >
          Refresh
        </UButton>
        <UButton
          color="primary"
          to="/transactions/new"
          icon="i-lucide-plus"
        >
          Add Transaction
        </UButton>
      </div>
    </div>

    <!-- Filters -->
    <UCard
      v-show="isFiltersOpen"
      class="mb-8 shadow-sm border-gray-200 dark:border-gray-800"
    >
      <div class="grid grid-cols-1 md:grid-cols-3 gap-4 items-end">
        <UFormField label="Account type">
          <USelect
            v-model="filters.accountType"
            :items="accountTypeOptions"
            class="w-full"
          />
        </UFormField>
        <UFormField label="User">
          <UInput
            v-model="filters.user"
            placeholder="e.g. alice"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Name">
          <UInput
            v-model="filters.name"
            placeholder="e.g. Checking"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Currency">
          <USelect
            v-model="filters.currency"
            :items="CURRENCY_OPTIONS"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="From transaction date"
          hint="Included"
        >
          <UInput
            v-model="filters.startDate"
            type="datetime-local"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="To transaction date"
          hint="Excluded"
        >
          <UInput
            v-model="filters.endDate"
            type="datetime-local"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Metadata key">
          <UInput
            v-model="filters.metadataKey"
            placeholder="e.g. orderId"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Metadata value">
          <UInput
            v-model="filters.metadataValue"
            placeholder="Matched exactly"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Date order">
          <USelect
            v-model="sort"
            :items="sortOptions"
            class="w-full"
          />
        </UFormField>
      </div>
      <div class="mt-4 flex justify-end gap-3">
        <UButton
          color="neutral"
          variant="ghost"
          @click="clearFilters"
        >
          Clear Filters
        </UButton>
        <UButton
          color="neutral"
          variant="soft"
          @click="apply"
        >
          Apply Filters
        </UButton>
      </div>
    </UCard>

    <!-- Data Table -->
    <LedgerError
      v-if="error"
      :error="error"
      title="Failed to load the register"
      @retry="refresh"
    />
    <UCard
      v-else
      class="shadow-sm border-gray-200 dark:border-gray-800"
    >
      <div
        v-if="loading"
        class="space-y-4 p-4"
      >
        <USkeleton
          v-for="i in 5"
          :key="i"
          class="h-12 w-full"
        />
      </div>

      <div
        v-else-if="rows.length === 0"
        class="text-center py-10"
      >
        <UIcon
          name="i-lucide-file-search"
          class="text-4xl text-gray-400 mb-2"
        />
        <h3 class="text-lg font-medium text-gray-900 dark:text-gray-100">
          {{ filtered ? 'No postings match these filters' : 'No postings in this register' }}
        </h3>
        <p class="text-gray-500">
          {{ filtered ? 'Nothing on the ledger matches.' : 'No posting has been recorded yet.' }}
        </p>
      </div>

      <div
        v-else
        class="divide-y divide-gray-200 dark:divide-gray-800"
      >
        <div
          v-for="row in rows"
          :key="row.id"
          class="p-4 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors"
        >
          <div class="flex justify-between items-center mb-2">
            <div class="flex items-center gap-3">
              <span class="text-xs font-semibold px-2 py-1 bg-gray-100 dark:bg-gray-800 rounded-md text-gray-600 dark:text-gray-400">
                {{ row.date }}
              </span>
              <span class="flex items-center gap-2 font-medium text-gray-900 dark:text-gray-100 font-mono">
                <span class="text-xs font-semibold text-gray-500 dark:text-gray-400">{{ row.account.type }}</span>
                <span>{{ row.account.user }}</span>
                <span>{{ row.account.name }}</span>
              </span>
            </div>
            <span class="text-xs text-gray-400 font-mono">TX: {{ row.transactionShortId }}</span>
          </div>

          <div class="flex justify-end text-sm mt-3 pl-2 sm:pl-10">
            <div class="flex items-center gap-4">
              <span :class="['font-medium w-24 text-right', row.amount.negative ? 'text-red-500' : 'text-emerald-500']">
                {{ row.amount.text }}
              </span>
              <span class="text-gray-400 w-40 text-right hidden sm:inline-block">
                Balance {{ row.balance.text }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- Pagination -->
      <div
        v-if="totalCount > 0"
        class="p-4 border-t border-gray-200 dark:border-gray-800 flex justify-between items-center"
      >
        <span class="text-sm text-gray-500">
          Showing {{ range.from }} to {{ range.to }} of {{ range.total }} postings
        </span>
        <UPagination
          v-model:page="page"
          :items-per-page="PAGE_SIZE"
          :total="totalCount"
        />
      </div>
    </UCard>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ListPostingsResponse, Posting } from '~/types/ledger'
import type { RegisterFilters } from '~/utils/postingFilters'

const isFiltersOpen = ref(false)

const accountTypeOptions = [
  { label: 'All account types', value: 'ALL' },
  ...ACCOUNT_TYPES
]

const sortOptions = [
  { label: 'Newest First', value: 'desc' },
  { label: 'Oldest First', value: 'asc' }
]

const emptyFilters = (): RegisterFilters => ({
  accountType: 'ALL',
  user: '',
  name: '',
  currency: 'ALL',
  startDate: '',
  endDate: '',
  metadataKey: '',
  metadataValue: '',
  ascending: false
})

const filters = ref<RegisterFilters>(emptyFilters())

const sort = computed({
  get: () => filters.value.ascending ? 'asc' : 'desc',
  set: (value: string) => {
    filters.value.ascending = value === 'asc'
  }
})

const {
  rows: postings,
  loading,
  error,
  refresh,
  apply,
  page,
  totalCount,
  range,
  filtered
} = useLedgerQuery<ListPostingsResponse, Posting>({
  key: 'register',
  path: '/postings/query',
  body: (page, pageSize) => toListPostingsRequest(filters.value, page, pageSize),
  rows: response => response.postings,
  pageSize: PAGE_SIZE
})

const rows = computed(() => toPostingRows(postings.value))

const clearFilters = () => {
  filters.value = emptyFilters()
  return apply()
}
</script>
