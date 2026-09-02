<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <div>
        <h1 class="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
          Trial Balance
        </h1>
        <p class="text-gray-500 mt-1">
          Every account's current balance. Add a filter to narrow it.
        </p>
      </div>
      <UButton
        color="neutral"
        variant="soft"
        icon="i-lucide-refresh-cw"
        :loading="loading"
        @click="refresh"
      >
        Refresh
      </UButton>
    </div>

    <!-- Filters -->
    <UCard class="mb-6 shadow-sm border-gray-200 dark:border-gray-800">
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-8">
        <UFormField label="Account Type">
          <USelect
            v-model="filters.type"
            :items="accountTypeOptions"
            class="w-full"
            @update:model-value="refresh"
          />
        </UFormField>
        <UFormField label="Currency">
          <USelect
            v-model="filters.currency"
            :items="CURRENCY_OPTIONS"
            class="w-full"
            @update:model-value="refresh"
          />
        </UFormField>
        <UFormField label="User">
          <UInput
            v-model="filters.user"
            placeholder="Exact user"
            class="w-full"
            @change="refresh"
          />
        </UFormField>
        <UFormField label="Name">
          <UInput
            v-model="filters.name"
            placeholder="Exact name"
            class="w-full"
            @change="refresh"
          />
        </UFormField>
      </div>
    </UCard>

    <!-- Data Table -->
    <LedgerError
      v-if="error"
      :error="error"
      title="Failed to load the trial balance"
      @retry="refresh"
    />
    <UCard
      v-else
      class="shadow-sm border-gray-200 dark:border-gray-800"
      :ui="{ body: 'p-0 sm:p-0' }"
    >
      <div class="overflow-x-auto">
        <table class="w-full text-left border-collapse">
          <thead>
            <tr class="border-b border-gray-200 dark:border-gray-800 text-sm font-medium text-gray-500 dark:text-gray-400">
              <th class="p-4 font-medium">
                Account Type
              </th>
              <th class="p-4 font-medium">
                User
              </th>
              <th class="p-4 font-medium">
                Name
              </th>
              <th class="p-4 font-medium">
                Balance
              </th>
              <th class="p-4 font-medium">
                Last Updated
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-gray-800">
            <tr v-if="loading">
              <td
                colspan="5"
                class="p-8 text-center text-gray-500"
              >
                Loading balances...
              </td>
            </tr>
            <tr v-else-if="balances.length === 0">
              <td
                colspan="5"
                class="p-8 text-center text-gray-500"
              >
                No accounts match these filters
              </td>
            </tr>
            <tr
              v-for="(row, i) in balances"
              v-else
              :key="i"
              class="hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors"
            >
              <td class="p-4 font-medium text-gray-900 dark:text-white">
                {{ renderAccount(row.account).type }}
              </td>
              <td class="p-4 text-gray-900 dark:text-white">
                {{ renderAccount(row.account).user }}
              </td>
              <td class="p-4 text-gray-900 dark:text-white">
                {{ renderAccount(row.account).name }}
              </td>
              <td class="p-4">
                <span
                  :class="[
                    'font-semibold',
                    isNegativeMoney(row.balance) ? 'text-red-500 dark:text-red-400' : 'text-emerald-500 dark:text-emerald-400'
                  ]"
                >
                  {{ formatMoney(row.balance) }}
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
import { ref } from 'vue'
import type { AccountBalance, ListAccountBalancesResponse } from '~/types/ledger'
import type { BalanceFilterControls } from '~/utils/balanceFilters'

const filters = ref<BalanceFilterControls>({
  type: 'ALL',
  currency: 'ALL',
  user: '',
  name: ''
})

const accountTypeOptions = [
  { label: 'All Accounts', value: 'ALL' },
  ...ACCOUNT_TYPES
]

const { rows: balances, loading, error, refresh } = useLedgerQuery<ListAccountBalancesResponse, AccountBalance>({
  key: 'trial-balance',
  path: '/accounts/balance',
  body: () => toListAccountBalancesRequest(filters.value),
  rows: response => response.balances
})
</script>
