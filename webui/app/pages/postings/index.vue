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
          @click="fetchData"
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
          @click="applyFilters"
        >
          Apply Filters
        </UButton>
      </div>
    </UCard>

    <!-- Data Table -->
    <UCard class="shadow-sm border-gray-200 dark:border-gray-800">
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
          No postings in this register
        </h3>
        <p class="text-gray-500">
          No posting matches these filters.
        </p>
      </div>

      <div
        v-else
        class="divide-y divide-gray-200 dark:divide-gray-800"
      >
        <div
          v-for="row in rows"
          :key="row.posting.id"
          class="p-4 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors"
        >
          <div class="flex justify-between items-center mb-2">
            <div class="flex items-center gap-3">
              <span class="text-xs font-semibold px-2 py-1 bg-gray-100 dark:bg-gray-800 rounded-md text-gray-600 dark:text-gray-400">
                {{ new Date(row.posting.date).toLocaleString() }}
              </span>
              <span class="flex items-center gap-2 font-medium text-gray-900 dark:text-gray-100 font-mono">
                <span class="text-xs font-semibold text-gray-500 dark:text-gray-400">{{ row.account.type }}</span>
                <span>{{ row.account.user }}</span>
                <span>{{ row.account.name }}</span>
              </span>
            </div>
            <span class="text-xs text-gray-400 font-mono">TX: {{ row.posting.transactionId?.substring(0, 8) }}</span>
          </div>

          <div class="flex justify-end text-sm mt-3 pl-2 sm:pl-10">
            <div class="flex items-center gap-4">
              <span :class="['font-medium w-24 text-right', isNegativeMoney(row.posting.amount) ? 'text-red-500' : 'text-emerald-500']">
                {{ formatMoney(row.posting.amount) }}
              </span>
              <span class="text-gray-400 w-40 text-right hidden sm:inline-block">
                Balance {{ formatMoney(row.posting.balance) }}
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
          Showing {{ (page - 1) * PAGE_SIZE + 1 }} to {{ Math.min(page * PAGE_SIZE, totalCount) }} of {{ totalCount }} postings
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
import { ref, computed, watch, onMounted } from 'vue'
import type { ListPostingsResponse, Posting } from '~/types/ledger'
import type { RegisterFilters } from '~/utils/postingFilters'

const { fetchApi } = useLedgerApi()
const loading = ref(true)
const postings = ref<Posting[]>([])
const page = ref(1)
const PAGE_SIZE = 50
const totalCount = ref(0)
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

const rows = computed(() => postings.value.map(posting => ({
  posting,
  account: renderAccount(posting.account)
})))

const applyFilters = () => {
  page.value = 1
  fetchData()
}

const clearFilters = () => {
  filters.value = emptyFilters()
  page.value = 1
  fetchData()
}

watch(page, () => {
  fetchData()
})

const fetchData = async () => {
  loading.value = true
  try {
    const request = toListPostingsRequest(filters.value, page.value, PAGE_SIZE)
    const data = await fetchApi<ListPostingsResponse>('/postings/query', {
      method: 'POST',
      body: request
    })
    postings.value = data.postings || []
    totalCount.value = Number(data.totalCount ?? 0)
  } catch (err: any) {
    if (err.response?.status === 401) {
      useRouter().push('/login')
    } else {
      useToast().add({ title: 'Error fetching postings', description: err.message, color: 'error' })
    }
  } finally {
    loading.value = false
  }
}

onMounted(() => fetchData())
</script>
