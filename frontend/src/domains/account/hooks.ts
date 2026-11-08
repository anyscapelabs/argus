import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ARGUS_API_URL, entitlements, getKeyStatus, saveKey, signOut } from './api'

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
    retry: false,
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
    retry: false,
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
    : !status.data
      ? { status: 'absent' }
      : check.isPending
        ? { status: 'pending' }
        : check.isError
          ? { status: 'invalid', message: check.error?.message ?? 'The agent key could not be verified.' }
          : { status: 'valid' }

  return { pairing, save, leave, account: check.data }
}
