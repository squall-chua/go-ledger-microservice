<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <div>
        <h1 class="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
          Record Transaction
        </h1>
        <p class="text-gray-500 mt-1">
          Double-entry ledger format. Debits and credits must zero out.
        </p>
      </div>
      <div class="flex gap-3">
        <UButton
          to="/transactions"
          color="neutral"
          variant="soft"
          icon="i-lucide-arrow-left"
        >
          Back to Register
        </UButton>
      </div>
    </div>

    <UCard class="shadow-sm border-gray-200 dark:border-gray-800">
      <form
        class="space-y-6"
        @submit.prevent="submitTransaction"
      >
        <div class="grid grid-cols-1 md:grid-cols-3 gap-8">
          <UFormField
            label="Description / Note"
            required
            class="md:col-span-1"
          >
            <UInput
              v-model="form.note"
              placeholder="e.g. Purchase Groceries"
              class="w-full"
              required
            />
          </UFormField>
          <UFormField
            label="Transaction Date"
            class="md:col-span-1"
          >
            <UInput
              v-model="form.date"
              type="datetime-local"
              class="w-full"
            />
          </UFormField>
          <UFormField
            label="Currency"
            class="md:col-span-1"
          >
            <USelectMenu
              v-model="form.currency"
              :items="CURRENCIES"
              class="w-full"
            />
          </UFormField>
        </div>

        <UDivider label="Postings" />

        <div class="space-y-4">
          <div
            v-for="(posting, index) in form.postings"
            :key="index"
            class="p-4 bg-gray-50 dark:bg-gray-900/50 rounded-lg border border-gray-200 dark:border-gray-800 relative group"
          >
            <UButton
              v-if="form.postings.length > 2"
              color="error"
              variant="ghost"
              icon="i-lucide-x"
              class="absolute -top-2 -right-2 bg-white dark:bg-gray-900 shadow-sm border border-gray-200 dark:border-gray-800 rounded-full h-8 w-8 !p-0 hidden group-hover:flex items-center justify-center z-10"
              @click="removePosting(index)"
            />

            <div class="grid grid-cols-1 md:grid-cols-4 gap-8">
              <UFormField
                label="Account Type"
                class="md:col-span-1"
              >
                <USelect
                  v-model="posting.type"
                  :items="ACCOUNT_TYPES"
                  class="w-full"
                />
              </UFormField>
              <UFormField
                label="Account User"
                class="md:col-span-1"
              >
                <UInput
                  v-model="posting.user"
                  placeholder="*"
                  class="w-full"
                  required
                />
              </UFormField>
              <UFormField
                label="Account Name"
                class="md:col-span-1"
              >
                <UInput
                  v-model="posting.name"
                  placeholder="e.g. Checking"
                  class="w-full"
                  required
                />
              </UFormField>
              <UFormField
                label="Amount"
                class="md:col-span-1"
              >
                <UInput
                  v-model.number="posting.amount"
                  type="number"
                  step="0.01"
                  placeholder="0.00"
                  class="w-full"
                  required
                />
              </UFormField>
            </div>
          </div>
        </div>

        <div class="flex justify-between items-center bg-gray-50 dark:bg-gray-900/50 p-4 rounded-lg border border-gray-200 dark:border-gray-800">
          <UButton
            color="neutral"
            variant="soft"
            icon="i-lucide-plus"
            @click="addPosting"
          >
            Add Posting
          </UButton>

          <div class="flex items-center gap-4">
            <span class="text-sm font-medium text-gray-500">Balance Checker:</span>
            <span :class="['text-lg font-bold', balanceSum === 0 ? 'text-emerald-500' : 'text-red-500']">
              Sum = {{ balanceSum }} <span class="text-sm font-normal text-gray-500">({{ form.currency }})</span>
            </span>
          </div>
        </div>

        <div class="flex justify-end pt-4 border-t border-gray-200 dark:border-gray-800">
          <UButton
            type="submit"
            color="primary"
            size="lg"
            :loading="submitting"
            :disabled="balanceSum !== 0 || form.note.trim() === ''"
          >
            Commit Transaction
          </UButton>
        </div>
      </form>
    </UCard>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const { fetchApi } = useLedgerApi()
const toast = useToast()
const router = useRouter()

const getDefaultPosting = () => ({
  type: 'ACCOUNT_TYPE_ASSETS',
  user: '*',
  name: '',
  amount: 0
})

const form = ref({
  note: '',
  date: '',
  currency: 'USD',
  postings: [
    { type: 'ACCOUNT_TYPE_ASSETS', user: '*', name: '', amount: 0 },
    { type: 'ACCOUNT_TYPE_EXPENSES', user: '*', name: '', amount: 0 }
  ]
})

const submitting = ref(false)

const balanceSum = computed(() => {
  let sum = 0
  for (const p of form.value.postings) {
    sum += (p.amount || 0)
  }
  return Number(sum.toFixed(2)) // Avoid floating point inaccuracies
})

const addPosting = () => {
  form.value.postings.push(getDefaultPosting())
}

const removePosting = (idx: number) => {
  if (form.value.postings.length > 2) {
    form.value.postings.splice(idx, 1)
  }
}

const submitTransaction = async () => {
  // Client side validation
  if (isNaN(balanceSum.value) || balanceSum.value !== 0) {
    toast.add({ title: 'Invalid Postings', description: 'Double-entry requires the sum of all amounts (per currency) to equal zero.', color: 'error' })
    return
  }

  submitting.value = true
  try {
    const payload = {
      idempotency_key: crypto.randomUUID(),
      note: form.value.note,
      date: form.value.date ? new Date(form.value.date).toISOString() : new Date().toISOString(),
      postings: form.value.postings.map(p => ({
        account: {
          type: p.type,
          name: p.name,
          user: p.user || '*' // Allow all user or generic
        },
        amount: {
          currencyCode: form.value.currency,
          units: Math.trunc(p.amount || 0),
          nanos: Math.round(((p.amount || 0) - Math.trunc(p.amount || 0)) * 1e9)
        }
      }))
    }

    await fetchApi('/transactions', {
      method: 'POST',
      body: payload
    })

    toast.add({ title: 'Success', description: 'Transaction successfully recorded.', icon: 'i-lucide-circle-check' })
    router.push('/transactions')
  } catch (err: any) {
    toast.add({
      title: 'Server Error',
      description: err.response?._data?.message || err.message || 'An unknown error occurred.',
      color: 'error'
    })
  } finally {
    submitting.value = false
  }
}
</script>
