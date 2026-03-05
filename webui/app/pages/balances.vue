<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <div>
        <h1 class="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">Account Balances</h1>
        <p class="text-gray-500 mt-1">View your current ledgers and their running balances.</p>
      </div>
      <UButton color="neutral" variant="soft" icon="i-lucide-refresh-cw" @click="fetchData" :loading="loading">
        Refresh
      </UButton>
    </div>

    <!-- Filters -->
    <UCard class="mb-6 shadow-sm border-gray-200 dark:border-gray-800">
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-8">
        <UFormField label="Account Type">
          <USelect v-model="filters.type" :items="accountTypeOptions" class="w-full" />
        </UFormField>
        <UFormField label="Currency">
          <USelect v-model="filters.currency" :items="CURRENCY_OPTIONS" class="w-full" />
        </UFormField>
        <UFormField label="User">
          <UInput v-model="filters.user" placeholder="Filter user..." class="w-full" />
        </UFormField>
        <UFormField label="Account Name">
          <UInput v-model="filters.name" placeholder="Filter name..." class="w-full" />
        </UFormField>
      </div>
    </UCard>

    <!-- Data Table -->
    <UCard class="shadow-sm border-gray-200 dark:border-gray-800" :ui="{ body: 'p-0 sm:p-0' }">
      <div class="overflow-x-auto">
        <table class="w-full text-left border-collapse">
          <thead>
            <tr class="border-b border-gray-200 dark:border-gray-800 text-sm font-medium text-gray-500 dark:text-gray-400">
              <th class="p-4 font-medium">Account Path</th>
              <th class="p-4 font-medium">Balance</th>
              <th class="p-4 font-medium">Last Updated</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-gray-800">
            <tr v-if="loading">
              <td colspan="3" class="p-8 text-center text-gray-500">Loading balances...</td>
            </tr>
            <tr v-else-if="filteredBalances.length === 0">
              <td colspan="3" class="p-8 text-center text-gray-500">No account records found</td>
            </tr>
            <tr v-else v-for="row in filteredBalances" :key="row.account?.name + row.account?.type" class="hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors">
              <td class="p-4 font-medium text-gray-900 dark:text-white">
                {{ formatAccountType(row.account?.type) }}:{{ row.account?.user || '*' }}:{{ row.account?.name || '*' }}
              </td>
              <td class="p-4">
                <span :class="[
                  'font-semibold',
                  isNegative(row.balance?.units) ? 'text-red-500 dark:text-red-400' : 'text-emerald-500 dark:text-emerald-400'
                ]">
                  {{ formatCurrency(row.balance?.units, row.balance?.currencyCode) }}
                </span>
              </td>
              <td class="p-4 text-sm text-gray-500 dark:text-gray-400">
                {{ new Date(row.updatedAt).toLocaleString() }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </UCard>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'

const { fetchApi } = useLedgerApi()
const loading = ref(true)
const rawBalances = ref<any[]>([])

const filters = ref({
  type: 'ALL',
  currency: 'ALL',
  user: '',
  name: ''
})

const accountTypeOptions = [
  { label: 'All Accounts', value: 'ALL' },
  ...ACCOUNT_TYPES
]

const isNegative = (units: number | string) => {
  return parseInt(String(units), 10) < 0
}

const formatCurrency = (amount: number | string, currency: string = 'USD') => {
  const val = parseInt(String(amount || 0), 10)
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(val)
}

const filteredBalances = computed(() => {
  return rawBalances.value.filter(b => {
    let matchType = true
    if (filters.value.type !== 'ALL') {
      matchType = formatAccountType(b.account?.type) === formatAccountType(filters.value.type)
    }
    let matchCurrency = true
    if (filters.value.currency && filters.value.currency !== 'ALL') {
      matchCurrency = b.balance?.currencyCode?.toLowerCase().includes(filters.value.currency.toLowerCase())
    }
    let matchUser = true
    if (filters.value.user) {
      const uStr = b.account?.user || '*'
      matchUser = uStr.toLowerCase().includes(filters.value.user.toLowerCase())
    }
    let matchName = true
    if (filters.value.name) {
      const nStr = b.account?.name || '*'
      matchName = nStr.toLowerCase().includes(filters.value.name.toLowerCase())
    }
    return matchType && matchCurrency && matchUser && matchName
  })
})

const fetchData = async () => {
  loading.value = true
  try {
    const data: any = await fetchApi('/accounts/balance', {
      method: 'POST',
      body: {
        account: {
          type: filters.value.type === 'ALL' ? 'ACCOUNT_TYPE_UNSPECIFIED' : filters.value.type
        },
        currency: filters.value.currency === 'ALL' ? '' : filters.value.currency
      }
    })
    rawBalances.value = data.balances || []
  } catch (err: any) {
    if (err.response?.status === 401) {
      useRouter().push('/login')
    } else {
      useToast().add({ title: 'Error', description: err.message, color: 'error' })
    }
  } finally {
    loading.value = false
  }
}

onMounted(() => fetchData())
</script>
