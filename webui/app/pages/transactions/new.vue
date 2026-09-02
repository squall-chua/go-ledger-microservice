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
          Back to Transactions
        </UButton>
      </div>
    </div>

    <div
      v-if="replayed"
      ref="replayedBanner"
      class="mb-6 p-4 rounded-lg border border-amber-200 dark:border-amber-900 bg-amber-50 dark:bg-amber-950/40"
    >
      <div class="flex items-center gap-2 mb-2">
        <UIcon
          name="i-lucide-history"
          class="text-amber-600 dark:text-amber-400"
        />
        <p class="text-sm font-medium text-amber-700 dark:text-amber-400">
          Nothing new was recorded: this idempotency key had already recorded this entry.
        </p>
      </div>
      <p class="text-sm text-amber-700 dark:text-amber-400 mb-4">
        The ledger returned the transaction below rather than writing a second one. Do not retry with a
        fresh key — that would record this entry twice.
      </p>

      <div class="p-4 bg-white dark:bg-gray-900 rounded-lg border border-amber-200 dark:border-amber-900">
        <div class="flex justify-between items-start mb-2">
          <div class="flex items-center gap-3">
            <span class="text-xs font-semibold px-2 py-1 bg-gray-100 dark:bg-gray-800 rounded-md text-gray-600 dark:text-gray-400">
              {{ new Date(replayed.date).toLocaleString() }}
            </span>
            <span class="font-medium text-gray-900 dark:text-gray-100">{{ replayed.note }}</span>
          </div>
          <span class="text-xs text-gray-400 font-mono">{{ replayed.id }}</span>
        </div>

        <div class="space-y-1 mt-3 pl-2 sm:pl-10">
          <div
            v-for="posting in replayed.postings"
            :key="posting.id"
            class="flex justify-between text-sm"
          >
            <span class="text-gray-600 dark:text-gray-300 font-mono flex flex-wrap gap-x-3">
              <span class="text-gray-400 dark:text-gray-500">{{ renderAccount(posting.account).type }}</span>
              <span>{{ renderAccount(posting.account).user }}</span>
              <span>{{ renderAccount(posting.account).name }}</span>
            </span>
            <span :class="['font-medium w-24 text-right', isNegativeMoney(posting.amount) ? 'text-red-500' : 'text-emerald-500']">
              {{ formatMoney(posting.amount) }}
            </span>
          </div>
        </div>
      </div>
    </div>

    <UCard class="shadow-sm border-gray-200 dark:border-gray-800">
      <form
        class="space-y-6"
        @submit.prevent="submitTransaction"
      >
        <div class="grid grid-cols-1 md:grid-cols-3 gap-8">
          <UFormField
            label="Note"
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
            hint="Blank lets the ledger stamp it"
            class="md:col-span-1"
          >
            <UInput
              v-model="form.date"
              type="datetime-local"
              class="w-full"
            />
          </UFormField>
          <UFormField
            label="Idempotency Key"
            hint="Paste one or generate it"
            class="md:col-span-1"
          >
            <div class="flex gap-2">
              <UInput
                v-model="form.idempotencyKey"
                placeholder="e.g. a UUID from the system that asked for this entry"
                class="w-full"
              />
              <UButton
                color="neutral"
                variant="soft"
                icon="i-lucide-refresh-cw"
                @click="generateIdempotencyKey"
              >
                Generate
              </UButton>
            </div>
          </UFormField>
        </div>

        <USeparator label="Postings" />

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

            <div class="grid grid-cols-1 md:grid-cols-5 gap-8">
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
                label="User"
                class="md:col-span-1"
              >
                <UInput
                  v-model="posting.user"
                  placeholder="e.g. alice"
                  class="w-full"
                />
              </UFormField>
              <UFormField
                label="Name"
                class="md:col-span-1"
              >
                <UInput
                  v-model="posting.name"
                  placeholder="e.g. Checking"
                  class="w-full"
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
              <UFormField
                label="Currency"
                class="md:col-span-1"
              >
                <USelectMenu
                  v-model="posting.currency"
                  :items="CURRENCIES"
                  class="w-full"
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
            <span :class="['text-lg font-bold', sumNanos === 0 ? 'text-emerald-500' : 'text-red-500']">
              Sum = {{ sumNanos / 1e9 }} <span class="text-sm font-normal text-gray-500">({{ form.postings[0]?.currency }})</span>
            </span>
          </div>
        </div>

        <USeparator label="Metadata" />

        <div class="space-y-4">
          <p
            v-if="form.metadata.length === 0"
            class="text-sm text-gray-500"
          >
            No metadata. Add a key and value pair to trace this entry back to what caused it.
          </p>
          <div
            v-for="(pair, index) in form.metadata"
            :key="index"
            class="flex items-end gap-4"
          >
            <UFormField
              label="Key"
              class="flex-1"
            >
              <UInput
                v-model="pair.key"
                placeholder="e.g. source"
                class="w-full"
              />
            </UFormField>
            <UFormField
              label="Value"
              class="flex-1"
            >
              <UInput
                v-model="pair.value"
                placeholder="e.g. manual-correction"
                class="w-full"
              />
            </UFormField>
            <UButton
              color="error"
              variant="soft"
              icon="i-lucide-x"
              @click="removeMetadata(index)"
            />
          </div>
          <UButton
            color="neutral"
            variant="soft"
            icon="i-lucide-plus"
            @click="addMetadata"
          >
            Add Metadata
          </UButton>
        </div>

        <div
          v-if="reasons.length > 0"
          class="p-4 rounded-lg border border-red-200 dark:border-red-900 bg-red-50 dark:bg-red-950/40"
        >
          <p class="text-sm font-medium text-red-600 dark:text-red-400 mb-2">
            Before this entry can be recorded:
          </p>
          <ul class="list-disc list-inside text-sm text-red-600 dark:text-red-400 space-y-1">
            <li
              v-for="reason in reasons"
              :key="reason"
            >
              {{ reason }}
            </li>
          </ul>
        </div>

        <div class="flex justify-end pt-4 border-t border-gray-200 dark:border-gray-800">
          <UButton
            type="submit"
            color="primary"
            size="lg"
            :loading="submitting"
            :disabled="reasons.length > 0"
          >
            Commit Transaction
          </UButton>
        </div>
      </form>
    </UCard>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import type { RecordTransactionResponse, Transaction } from '~/types/ledger'
import type { PostingRow, TransactionFormValues } from '~/utils/transactionForm'

const { fetchApi } = useLedgerApi()
const toast = useToast()
const router = useRouter()

const getDefaultPosting = (): PostingRow => ({
  type: 'ACCOUNT_TYPE_ASSETS',
  user: '',
  name: '',
  amount: 0,
  currency: 'USD'
})

const form = ref<TransactionFormValues>({
  note: '',
  date: '',
  idempotencyKey: '',
  postings: [
    getDefaultPosting(),
    { ...getDefaultPosting(), type: 'ACCOUNT_TYPE_EXPENSES' }
  ],
  metadata: []
})

const submitting = ref(false)

// The transaction the ledger returned instead of recording a new one. Held so
// the operator can see what is already there, which is why a replay stays on
// the page rather than routing away like a fresh entry does.
const replayed = ref<Transaction | null>(null)
const replayedBanner = ref<HTMLElement | null>(null)

// The running sum is kept in whole nanos, the same integers the postings are
// sent as, so what the operator watches reach zero is what the ledger checks.
const sumNanos = computed(() => {
  return form.value.postings.reduce((total, posting) => total + amountToNanos(posting.amount), 0)
})

const reasons = computed(() => validateTransactionForm(form.value))

const generateIdempotencyKey = () => {
  // crypto.randomUUID only exists in a secure context, so over plain HTTP on a
  // host that is not localhost it is undefined. The fallback is not random
  // enough to be a secret, and does not need to be: an idempotency key only has
  // to be unique among this operator's submissions, and it is theirs to edit.
  form.value.idempotencyKey = crypto.randomUUID?.()
    ?? `key-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

// Generated on the client only: a key generated during SSR would not match the
// one the browser generates on hydration.
onMounted(() => generateIdempotencyKey())

const addPosting = () => {
  form.value.postings.push(getDefaultPosting())
}

const removePosting = (idx: number) => {
  if (form.value.postings.length > 2) {
    form.value.postings.splice(idx, 1)
  }
}

const addMetadata = () => {
  form.value.metadata.push({ key: '', value: '' })
}

const removeMetadata = (idx: number) => {
  form.value.metadata.splice(idx, 1)
}

const submitTransaction = async () => {
  // Cleared before the checks below, so a banner from an earlier replay cannot
  // sit there describing an unrelated transaction while this submit is refused.
  replayed.value = null

  // The local checks are a convenience; the ledger stays the authority.
  if (reasons.value.length > 0) {
    return
  }

  submitting.value = true
  try {
    const response = await fetchApi<RecordTransactionResponse>('/transactions', {
      method: 'POST',
      body: toRecordTransactionRequest(form.value)
    })

    if (response.replayed) {
      replayed.value = response.transaction
      // The banner is at the top of a long form and the Record button is at the
      // bottom, so without this the operator only ever sees the toast.
      await nextTick()
      replayedBanner.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      toast.add({
        title: 'Already Recorded',
        description: 'This idempotency key had already recorded this entry. Nothing new was written.',
        icon: 'i-lucide-history',
        color: 'warning'
      })
      return
    }

    toast.add({ title: 'Success', description: 'Transaction successfully recorded.', icon: 'i-lucide-circle-check' })
    router.push('/transactions')
  } catch (err: any) {
    toast.add({
      title: 'Transaction Refused',
      description: describeLedgerError(err),
      color: 'error'
    })
  } finally {
    submitting.value = false
  }
}
</script>
