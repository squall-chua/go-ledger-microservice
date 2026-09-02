<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
        Overview
      </h1>
      <USelectMenu
        v-model="selectedCurrency"
        :items="CURRENCIES"
        class="w-32"
        @update:model-value="refresh"
      />
    </div>

    <div
      v-if="loading"
      class="grid grid-cols-1 md:grid-cols-3 gap-6"
    >
      <USkeleton
        v-for="i in 3"
        :key="i"
        class="h-32 rounded-xl"
      />
    </div>

    <LedgerError
      v-else-if="error"
      :error="error"
      title="Failed to load overview"
      @retry="refresh"
    />

    <div
      v-else
      class="grid grid-cols-1 md:grid-cols-3 gap-6"
    >
      <UCard
        class="bg-gradient-to-br from-blue-500 to-indigo-600 text-white !border-0 !shadow-lg !ring-0 rounded-xl hover:shadow-xl transition-shadow"
      >
        <div class="flex flex-col">
          <span class="text-blue-100 font-medium tracking-wide text-sm uppercase">Total Assets</span>
          <div class="mt-2 text-3xl font-bold">
            {{ totals.assets }}
          </div>
          <div class="mt-4 flex items-center text-sm text-blue-100">
            <UIcon
              name="i-lucide-trending-up"
              class="mr-1"
            />
            Healthy Reserve
          </div>
        </div>
      </UCard>

      <UCard
        class="bg-gradient-to-br from-emerald-500 to-teal-600 text-white !border-0 !shadow-lg !ring-0 rounded-xl hover:shadow-xl transition-shadow"
      >
        <div class="flex flex-col">
          <span class="text-emerald-100 font-medium tracking-wide text-sm uppercase">Total Revenue</span>
          <div class="mt-2 text-3xl font-bold">
            {{ totals.revenue }}
          </div>
          <div class="mt-4 flex items-center text-sm text-emerald-100">
            <UIcon
              name="i-lucide-banknote"
              class="mr-1"
            />
            Income Streams
          </div>
        </div>
      </UCard>

      <UCard
        class="bg-gradient-to-br from-rose-500 to-red-600 text-white !border-0 !shadow-lg !ring-0 rounded-xl hover:shadow-xl transition-shadow"
      >
        <div class="flex flex-col">
          <span class="text-rose-100 font-medium tracking-wide text-sm uppercase">Total Expenses</span>
          <div class="mt-2 text-3xl font-bold">
            {{ totals.expenses }}
          </div>
          <div class="mt-4 flex items-center text-sm text-rose-100">
            <UIcon
              name="i-lucide-trending-down"
              class="mr-1"
            />
            Outgoing Capital
          </div>
        </div>
      </UCard>
    </div>

    <div class="mt-8">
      <UCard class="rounded-xl shadow-sm border-gray-200 dark:border-gray-800">
        <template #header>
          <div class="flex items-center justify-center">
            <h3 class="text-lg font-semibold dark:text-gray-100 text-center w-full">
              Quick Actions
            </h3>
          </div>
        </template>
        <div class="flex gap-4 justify-center">
          <UButton
            to="/balances"
            color="neutral"
            variant="soft"
            icon="i-lucide-pie-chart"
            class="justify-center"
          >
            View Balances
          </UButton>
          <UButton
            to="/transactions"
            color="neutral"
            variant="soft"
            icon="i-lucide-list"
            class="justify-center"
          >
            View Register
          </UButton>
          <UButton
            to="/postings"
            color="neutral"
            variant="soft"
            icon="i-lucide-users"
            class="justify-center"
          >
            View Postings
          </UButton>
          <UButton
            to="/transactions/new"
            color="primary"
            icon="i-lucide-plus"
            class="justify-center"
          >
            Add Transaction
          </UButton>
        </div>
      </UCard>
    </div>

    <!-- Account Balances Table -->
    <div class="mt-8">
      <UCard
        class="rounded-xl shadow-sm border-gray-200 dark:border-gray-800"
        :ui="{ body: 'p-0 sm:p-0', header: 'py-3' }"
      >
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
              Account Balances
            </h3>
            <span class="text-xs text-gray-500">{{ selectedCurrency }}</span>
          </div>
        </template>
        <div class="overflow-x-auto max-h-64">
          <table class="w-full text-left border-collapse">
            <tbody class="divide-y divide-gray-200 dark:divide-gray-800">
              <tr v-if="loading">
                <td
                  colspan="4"
                  class="py-2 px-4 text-center text-xs text-gray-500"
                >
                  Loading balances...
                </td>
              </tr>
              <tr v-else-if="rows.length === 0">
                <td
                  colspan="4"
                  class="py-2 px-4 text-center text-xs text-gray-500"
                >
                  No balances found for {{ selectedCurrency }}
                </td>
              </tr>
              <tr
                v-for="(row, i) in rows"
                v-else
                :key="i"
                class="hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors"
              >
                <td class="py-2 px-4 text-xs font-medium text-gray-700 dark:text-gray-300">
                  {{ row.account.type }}
                </td>
                <td class="py-2 px-4 text-xs text-gray-700 dark:text-gray-300">
                  {{ row.account.user }}
                </td>
                <td class="py-2 px-4 text-xs text-gray-700 dark:text-gray-300">
                  {{ row.account.name }}
                </td>
                <td class="py-2 px-4 text-xs text-right">
                  <span
                    :class="[
                      'font-medium',
                      row.balance.negative ? 'text-red-500 dark:text-red-400' : 'text-emerald-500 dark:text-emerald-400'
                    ]"
                  >
                    {{ row.balance.text }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </UCard>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { AccountBalance, ListAccountBalancesResponse } from '~/types/ledger'

const selectedCurrency = ref('USD')

const { rows: balances, loading, error, refresh } = useLedgerQuery<ListAccountBalancesResponse, AccountBalance>({
  key: 'overview',
  path: '/accounts/balance',
  body: () => toListAccountBalancesRequest({
    type: 'ALL',
    currency: selectedCurrency.value,
    user: '',
    name: ''
  }),
  rows: response => response.balances
})

const rows = computed(() => toBalanceRows(balances.value))
const totals = computed(() => toBalanceTotals(balances.value, selectedCurrency.value))
</script>
