import { ref } from 'vue'

const token = ref<string | null>(null)

export const useAuth = () => {
  if (import.meta.client && token.value === null) {
    token.value = localStorage.getItem('ledger_jwt')
  }

  const setToken = (newToken: string) => {
    token.value = newToken
    if (import.meta.client) {
      localStorage.setItem('ledger_jwt', newToken)
    }
  }

  const logout = () => {
    token.value = null
    if (import.meta.client) {
      localStorage.removeItem('ledger_jwt')
    }
  }

  return { token, setToken, logout }
}

export const useLedgerApi = () => {
  const { token, logout } = useAuth()

  const fetchApi = $fetch.create({
    baseURL: '/api/v1/ledger',
    onRequest({ options }) {
      if (token.value) {
        const headers = new Headers(options.headers)
        headers.set('Authorization', `Bearer ${token.value}`)
        options.headers = headers
      }
    },
    async onResponseError({ response }) {
      // 401 is a token the ledger will not read at all; 403 is one it reads
      // but which lacks the scope the page needs. Neither is something the
      // operator can fix on the page they are on, and the only place to
      // supply a better token is the login page, so send them there rather
      // than leaving a dead page behind.
      if (response.status !== 401 && response.status !== 403) {
        return
      }
      // The login page validates a pasted token with a call of its own. It
      // has to see the failure to report it, so it is left alone.
      const router = useRouter()
      if (router.currentRoute.value.path === '/login') {
        return
      }
      logout()
      await router.push('/login')
    }
  })

  return { fetchApi }
}
