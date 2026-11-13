import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ARGUS_API_URL, entitlements, getKeyStatus, saveKey, signOut } from './api'

// Auth failures (bad key, revoked key, unverified email) are fatal; retrying
// them just burns the 24h rate-limit budget. Everything else (boot with the
// backend cold, a dropped connection) is worth two more attempts.
const NON_RETRYABLE = /(invalid|revoked|expired|401|403|not valid|no longer valid|not verified)/i
function retryUnlessAuthFailure(failureCount: number, error: unknown): boolean {
  const message = errorMessage(error)
  if (NON_RETRYABLE.test(message)) return false
  return failureCount < 2
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  if (typeof error === 'string') return error
  if (error && typeof error === 'object' && 'message' in error) {
    const m = (error as { message: unknown }).message
    if (typeof m === 'string') return m
  }
  return String(error)
}

// The pairing state the shell's router branches on: does the OS keyring hold
// an agent key, with four exclusive outcomes — absent (never paired), pending
// (checking), valid (paired and the backend confirms the key), invalid.
export type PairingState =
  | { status: 'pending' }
  | { status: 'absent' }
  | { status: 'invalid'; message: string }
  | { status: 'valid' }

// useAccount exposes the paired account snapshot (identity + plan + allowlist)
// without coupling callers to the pairing state machine. Returns undefined
// while loading or when no key is paired.
export function useAccount() {
  return useQuery({
    queryKey: ['account', 'entitlements', ARGUS_API_URL],
    queryFn: async () => {
      const res = await entitlements()
      if (res.error) throw new Error(res.error.message)
      return res.account
    },
    staleTime: 60_000,
    retry: retryUnlessAuthFailure,
  })
}

export function usePairing(initialReady: boolean) {
  const qc = useQueryClient()
  const status = useQuery({
    queryKey: ['account', 'key-status'],
    enabled: initialReady,
    staleTime: Infinity,
    queryFn: async () => {
      const res = await getKeyStatus()
      if (res.error) throw new Error(res.error.message)
      return res.status?.hasKey ?? false
    },
  })

  const check = useQuery({
    queryKey: ['account', 'entitlements', ARGUS_API_URL],
    // Only worth a network round-trip once the keyring says a key exists.
    enabled: status.data === true,
    retry: retryUnlessAuthFailure,
    staleTime: 60_000,
    queryFn: async () => {
      const res = await entitlements()
      if (res.error) throw new Error(res.error.message)
      return res.account
    },
  })

  const save = useMutation({
    mutationFn: async (key: string) => {
      const res = await saveKey(key)
      if (res.error) throw new Error(res.error.message)
      // Prove the key *inside* the mutation so a bad key rejects the
      // mutation: the proof runs even while the query below is still
      // gated on the refreshed key status.
      const proof = await entitlements()
      if (proof.error || !proof.account) {
        throw new Error(
          proof.error?.message ?? 'The agent key could not be verified.',
        )
      }
      // Order matters: the keyring write just invalidated the cached key
      // status (`staleTime` was Infinity), and the entitlements query only
      // awakens once the status reads `hasKey: true`. Refresh status first,
      // then prove the key — otherwise the shell sees a fresh key with a
      // stale `absent` designation and bounces the operator back here.
      await status.refetch()
      await check.refetch()
    },
  })

  const leave = useMutation({
    mutationFn: async () => {
      const res = await signOut()
      if (res.error) throw new Error(res.error.message)
    },
    onSettled: async () => {
      qc.setQueryData(['account', 'entitlements', ARGUS_API_URL], undefined)
      await status.refetch()
    },
  })

  const computing = status.isPending
  // A key that exists but has not yet been (re)confirmed counts as pending —
  // this covers the post-save moment before the entitlements proof lands.
  const pairing: PairingState = computing
    ? { status: 'pending' }
    : status.isError
      // A keyring/storage failure is not "no key": surface it instead of
      // masquerading as an unpaired machine.
      ? {
          status: 'invalid',
          message:
            errorMessage(status.error) ||
            'The agent key could not be read from this device.',
        }
      : !status.data
        ? { status: 'absent' }
        : check.isPending
          ? { status: 'pending' }
          : check.isError
            ? { status: 'invalid', message: errorMessage(check.error) || 'The agent key could not be verified.' }
            : { status: 'valid' }

  return { pairing, save, leave, account: check.data }
}
