<template>
  <div class="flex h-[calc(100vh-64px)] items-center justify-center">
    <UCard class="w-full max-w-md">
      <template #header>
        <h2 class="text-xl font-bold flex items-center justify-center gap-2">
          <UIcon name="i-lucide-lock" />
          Login to Ledger
        </h2>
        <p class="text-sm text-gray-500 text-center mt-1">
          Paste your JWT authentication token to continue.
        </p>
      </template>

      <form @submit.prevent="login">
        <div class="space-y-4">
          <UFormField label="JWT Token">
            <UTextarea
              v-model="jwt"
              type="text"
              placeholder="eyJhbGci..."
              autoresize
              :rows="4"
              class="w-full"
              required
            />
          </UFormField>

          <UButton
            type="submit"
            color="primary"
            block
            class="mt-4"
            :loading="loading"
          >
            Authenticate
          </UButton>
        </div>
      </form>
    </UCard>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const { setToken } = useAuth()
const toast = useToast()

const jwt = ref('')
const loading = ref(false)

const login = async () => {
  loading.value = true

  try {
    if (!jwt.value) throw new Error('Token is required')

    // Basic structural validation of JWT (Header.Payload.Signature)
    if (jwt.value.split('.').length !== 3) {
      throw new Error('Invalid JWT format.')
    }

    setToken(jwt.value.trim())

    // Validate token with backend before redirecting
    try {
      const { fetchApi } = useLedgerApi()
      await fetchApi('/accounts/balance', {
        method: 'POST',
        body: { account: {} }
      })
    } catch (apiErr: any) {
      setToken('') // Clear invalid token
      if (apiErr.response?.status === 401) {
        throw new Error('Invalid or expired token. The server rejected it.')
      }
      throw new Error(`Server connection failed: ${apiErr.message}`)
    }

    toast.add({
      title: 'Authenticated Successfully',
      description: 'You have logged into the Ledger Service.',
      icon: 'i-lucide-circle-check'
    })

    return navigateTo('/')
  } catch (error: any) {
    toast.add({
      title: 'Authentication Failed',
      description: error.message,
      color: 'error',
      icon: 'i-lucide-circle-alert'
    })
  } finally {
    loading.value = false
  }
}
</script>
