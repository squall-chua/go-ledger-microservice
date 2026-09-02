import { computed, ref, watch, type Ref, type WatchSource } from 'vue'

/** The part of a listing response this module reads for itself. */
interface ListResponse {
  /** `totalCount` is an int64, so protojson sends it as a string. */
  totalCount?: string | number
}

export interface LedgerQueryOptions<Res extends ListResponse, Row> {
  /**
   * Unique per page. Two pages read `/accounts/balance`, so the path alone
   * would have them share one cache entry.
   */
  key: string
  /** Path under the ledger's base URL, such as `/transactions/query`. */
  path: string
  /**
   * Builds the request. Called afresh for every read, so it sees the filters
   * as they are at that moment rather than as they were at setup.
   */
  body: (page: number, pageSize: number) => Record<string, unknown>
  /** Picks the rows out of the response. */
  rows: (response: Res) => Row[] | undefined
  /**
   * Sources that start a new read when they change. A page whose controls
   * fetch on their own passes them here; a page with an Apply button passes
   * none and calls `apply` instead.
   */
  watch?: WatchSource[]
  /** Sizes the request and the range. Only a paginated read needs it. */
  pageSize?: number
}

/**
 * One read of the ledger: the request, the rows, whether it is in flight, and
 * what to say when it fails.
 *
 * Every page used to carry its own copy of this, and the copies drifted: two
 * of them guarded against an overtaken response and two did not, and each of
 * the four said something different when a read failed.
 */
export const useLedgerQuery = <Res extends ListResponse, Row>(options: LedgerQueryOptions<Res, Row>) => {
  const { fetchApi } = useLedgerApi()
  const pageSize = options.pageSize ?? 0
  const page = ref(1)
  /** The request the last read sent, so `filtered` describes what is on screen. */
  const sent = ref<Record<string, unknown>>({})

  const { data, status, error, refresh } = useAsyncData<Res>(options.key, () => {
    const request = options.body(page.value, pageSize)
    sent.value = request
    return fetchApi<Res>(options.path, { method: 'POST', body: request })
  }, {
    // The token lives in localStorage, so a read from the server would go out
    // with no Authorization header at all. Every read waits for the client.
    server: false,
    lazy: true,
    // Two quick changes leave two reads in flight. Cancelling the older one is
    // what keeps a trial balance from showing rows for a filter the operator
    // has already moved off — silently wrong numbers.
    dedupe: 'cancel',
    // Without this, coming back to a page shows the rows from the last visit
    // while the fresh ones load. Stale rows under a fresh page number are the
    // same wrongness one navigation later.
    getCachedData: () => undefined,
    watch: options.watch
  })

  // Turning the page is a read. `apply` resets the page and reads once: doing
  // both separately is what made a single press of Apply fire two requests
  // whenever the operator was not already on page one.
  let quiet = false
  watch(page, () => {
    if (!quiet) {
      refresh()
    }
  }, { flush: 'sync' })

  const apply = () => {
    quiet = true
    page.value = 1
    quiet = false
    return refresh()
  }

  // useAsyncData describes its data with a mapped type that does not narrow
  // back to Res on its own, so read the response through the shape asked for.
  const response = computed(() => data.value as Res | null)
  const rows = computed(() => (response.value ? options.rows(response.value) ?? [] : []))
  const totalCount = computed(() => Number(response.value?.totalCount ?? 0))

  /** One-based, inclusive, and both zero on an empty read. */
  const range = computed(() => ({
    from: totalCount.value === 0 ? 0 : (page.value - 1) * pageSize + 1,
    to: Math.min(page.value * pageSize, totalCount.value),
    total: totalCount.value
  }))

  /**
   * Whether the read that produced these rows was narrowed, so an empty table
   * can say which kind of empty it is. Both paginated requests carry every
   * filter under `filter`; the balance request has no such key and neither of
   * its callers reads this.
   */
  const filtered = computed(() => sent.value.filter !== undefined)

  const loading = computed(() => status.value === 'idle' || status.value === 'pending')

  return { rows, loading, error, refresh, apply, page, totalCount, range, filtered } as {
    rows: Ref<Row[]>
    loading: Ref<boolean>
    error: Ref<Error | null>
    refresh: () => Promise<void>
    apply: () => Promise<void>
    page: Ref<number>
    totalCount: Ref<number>
    range: Ref<{ from: number, to: number, total: number }>
    filtered: Ref<boolean>
  }
}
