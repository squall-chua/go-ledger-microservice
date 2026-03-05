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
    const { token } = useAuth()

    const fetchApi = $fetch.create({
        baseURL: '/api/v1/ledger',
        onRequest({ options }) {
            if (token.value) {
                const headers = new Headers(options.headers)
                headers.set('Authorization', `Bearer ${token.value}`)
                options.headers = headers
            }
        }
    })

    return { fetchApi }
}
