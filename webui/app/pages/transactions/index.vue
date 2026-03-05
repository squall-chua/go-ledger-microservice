<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <div>
        <h1 class="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
          Transaction Register
        </h1>
        <p class="text-gray-500 mt-1">
          Chronological history of all ledger entries.
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
      <div class="grid grid-cols-4 gap-4 items-end">
        <UFormField label="Start Date">
          <UInput
            v-model="filters.start_date"
            type="datetime-local"
            class="w-full"
          />
        </UFormField>
        <UFormField label="End Date">
          <UInput
            v-model="filters.end_date"
            type="datetime-local"
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
        <UFormField label="Sort Order">
          <USelect
            v-model="filters.sort"
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
        v-else-if="transactions.length === 0"
        class="text-center py-10"
      >
        <UIcon
          name="i-lucide-file-search"
          class="text-4xl text-gray-400 mb-2"
        />
        <h3 class="text-lg font-medium text-gray-900 dark:text-gray-100">
          No transactions recorded
        </h3>
        <p class="text-gray-500">
          Your ledger history is empty.
        </p>
      </div>

      <div
        v-else
        class="divide-y divide-gray-200 dark:divide-gray-800"
      >
        <div
          v-for="tx in transactions"
          :key="tx.id"
          class="p-4 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors"
        >
          <div class="flex justify-between items-start mb-2">
            <div class="flex items-center gap-3">
              <span class="text-xs font-semibold px-2 py-1 bg-gray-100 dark:bg-gray-800 rounded-md text-gray-600 dark:text-gray-400">
                {{ new Date(tx.date || tx.createdAt).toLocaleString() }}
              </span>
              <span class="font-medium text-gray-900 dark:text-gray-100">{{ tx.note }}</span>
            </div>
            <span class="text-xs text-gray-400 font-mono">{{ tx.id?.substring(0, 8) }}</span>
          </div>

          <div class="space-y-1 mt-3 pl-2 sm:pl-10">
            <div
              v-for="posting in tx.postings"
              :key="posting.id"
              class="flex justify-between text-sm"
            >
              <span class="text-gray-600 dark:text-gray-300 font-mono">
                {{ formatAccountType(posting.account?.type) }}:{{ posting.account?.user || '*' }}:{{ posting.account?.name || '*' }}
              </span>
              <div class="flex items-center gap-4">
                <span :class="['font-medium w-24 text-right', isNegative(posting.amount?.units) ? 'text-red-500' : 'text-emerald-500']">
                  {{ formatCurrency(posting.amount?.units, posting.amount?.currencyCode) }}
                </span>
                <span class="text-gray-400 w-32 text-right hidden sm:inline-block">
                  (= {{ formatCurrency(posting.balance?.units, posting.balance?.currencyCode) }})
                </span>
              </div>
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
          Showing {{ (page - 1) * pageCount + 1 }} to {{ Math.min(page * pageCount, totalCount) }} of {{ totalCount }} transactions
        </span>
        <UPagination
          v-model="page"
          :page-count="pageCount"
          :total="totalCount"
        />
      </div>
    </UCard>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'

const { fetchApi } = useLedgerApi()
const loading = ref(true)
const transactions = ref<any[]>([])
const page = ref(1)
const pageCount = 50
const totalCount = ref(0)
const isFiltersOpen = ref(false)

const sortOptions = [
  { label: 'Newest First', value: 'desc' },
  { label: 'Oldest First', value: 'asc' }
]

const filters = ref({
  start_date: '',
  end_date: '',
  currency: 'ALL',
  sort: 'desc'
})

const applyFilters = () => {
  page.value = 1
  fetchData()
}

const clearFilters = () => {
  filters.value = {
    start_date: '',
    end_date: '',
    currency: 'ALL',
    sort: 'desc'
  }
  page.value = 1
  fetchData()
}

watch(page, () => {
  fetchData()
})

const isNegative = (units: number | string) => {
  return parseInt(String(units || 0), 10) < 0
}

const formatCurrency = (amount: number | string, currency: string = 'USD') => {
  const val = parseInt(String(amount || 0), 10)
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(val)
}

const fetchData = async () => {
  loading.value = true
  try {
    const filterPayload: any = {}
    if (filters.value.currency && filters.value.currency !== 'ALL') {
      filterPayload.currency = filters.value.currency
    }
    if (filters.value.start_date) {
      filterPayload.start_date = new Date(filters.value.start_date).toISOString()
    }
    if (filters.value.end_date) {
      filterPayload.end_date = new Date(filters.value.end_date).toISOString()
    }

    const data: any = await fetchApi('/transactions/query', {
      method: 'POST',
      body: {
        filter: filterPayload,
        page_size: pageCount,
        page_number: page.value,
        order_by_desc: filters.value.sort === 'desc'
      }
    })
    transactions.value = data.transactions || []
    totalCount.value = parseInt(data.totalCount || data.total_count || 0, 10)
  } catch (err: any) {
    if (err.response?.status === 401) {
      useRouter().push('/login')
    } else {
      useToast().add({ title: 'Error fetching transactions', description: err.message, color: 'error' })
    }
  } finally {
    loading.value = false
  }
}

onMounted(() => fetchData())
</script>
