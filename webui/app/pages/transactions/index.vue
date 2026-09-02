<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <div>
        <h1 class="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
          Transactions
        </h1>
        <p class="text-gray-500 mt-1">
          Every transaction recorded in the book, newest first.
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
      <div class="grid grid-cols-3 gap-4 items-end">
        <UFormField label="Start Date">
          <UInput
            v-model="filters.startDate"
            type="datetime-local"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="End Date"
          help="Excluded from the range."
        >
          <UInput
            v-model="filters.endDate"
            type="datetime-local"
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
        <UFormField
          label="Idempotency Key"
          help="Matches at most one transaction."
        >
          <UInput
            v-model="filters.idempotencyKey"
            placeholder="The key the write was sent with"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="Metadata Key"
          help="Blank filters nothing."
        >
          <UInput
            v-model="filters.metadataKey"
            placeholder="order_id"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="Metadata Value"
          help="Matched exactly, with the key."
        >
          <UInput
            v-model="filters.metadataValue"
            placeholder="A-42"
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
          {{ wasFiltered ? 'No transactions match these filters' : 'No transactions recorded' }}
        </h3>
        <p class="text-gray-500">
          {{ wasFiltered ? 'Nothing on the ledger matches. An idempotency key with no match means that write never landed.' : 'Your ledger history is empty.' }}
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
                {{ new Date(tx.date).toLocaleString() }}
              </span>
              <span class="font-medium text-gray-900 dark:text-gray-100">{{ tx.note }}</span>
            </div>
            <span class="text-xs text-gray-400 font-mono">{{ tx.id?.substring(0, 8) }}</span>
          </div>

          <div
            v-if="Object.keys(tx.metadata || {}).length > 0"
            class="flex flex-wrap gap-2 mt-2"
          >
            <span
              v-for="(value, key) in tx.metadata"
              :key="key"
              class="text-xs font-mono px-2 py-0.5 bg-gray-100 dark:bg-gray-800 rounded text-gray-600 dark:text-gray-400"
            >
              {{ key }} = {{ value }}
            </span>
          </div>

          <div class="space-y-1 mt-3 pl-2 sm:pl-10">
            <div
              v-for="posting in tx.postings"
              :key="posting.id"
              class="flex justify-between text-sm"
            >
              <span class="text-gray-600 dark:text-gray-300 font-mono flex flex-wrap gap-x-3">
                <span class="text-gray-400 dark:text-gray-500">{{ renderAccount(posting.account).type }}</span>
                <span>{{ renderAccount(posting.account).user }}</span>
                <span>{{ renderAccount(posting.account).name }}</span>
              </span>
              <div class="flex items-center gap-4">
                <span :class="['font-medium w-24 text-right', isNegativeMoney(posting.amount) ? 'text-red-500' : 'text-emerald-500']">
                  {{ formatMoney(posting.amount) }}
                </span>
                <span class="text-gray-400 w-32 text-right hidden sm:inline-block">
                  (= {{ formatMoney(posting.balance) }})
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
          Showing {{ (page - 1) * pageSize + 1 }} to {{ Math.min(page * pageSize, totalCount) }} of {{ totalCount }} transactions
        </span>
        <UPagination
          v-model:page="page"
          :items-per-page="pageSize"
          :total="totalCount"
        />
      </div>
    </UCard>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import type { ListTransactionsResponse, Transaction } from '~/types/ledger'
import type { TransactionFilterControls } from '~/utils/transactionFilters'

const { fetchApi } = useLedgerApi()
const loading = ref(true)
const transactions = ref<Transaction[]>([])
const page = ref(1)
const pageSize = 50
const totalCount = ref(0)
const isFiltersOpen = ref(false)
/** Whether the list on screen was narrowed, so the empty state can say so. */
const wasFiltered = ref(false)

const sortOptions = [
  { label: 'Newest First', value: 'newest' },
  { label: 'Oldest First', value: 'oldest' }
]

const emptyFilters = (): TransactionFilterControls => ({
  startDate: '',
  endDate: '',
  sort: 'newest',
  idempotencyKey: '',
  metadataKey: '',
  metadataValue: ''
})

const filters = ref(emptyFilters())

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
    const request = toListTransactionsRequest({
      ...filters.value,
      page: page.value,
      pageSize
    })
    wasFiltered.value = request.filter !== undefined

    const data = await fetchApi<ListTransactionsResponse>('/transactions/query', {
      method: 'POST',
      body: request
    })
    transactions.value = data.transactions || []
    totalCount.value = Number(data.totalCount)
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
