import { useEffect, useRef } from 'react'

function makeSignature<T>(value: T): string {
  return JSON.stringify(value, (_key, v) => {
    if (v instanceof Date) return v.toISOString()
    return v
  })
}

export function useSyncToStore<T>(data: T | undefined, syncFn: (data: T) => void) {
  const syncFnRef = useRef(syncFn)
  const lastSigRef = useRef<string | null>(null)
  syncFnRef.current = syncFn

  useEffect(() => {
    if (data !== undefined) {
      const nextSig = makeSignature(data)
      if (lastSigRef.current === nextSig) return
      lastSigRef.current = nextSig
      syncFnRef.current(data)
    }
  }, [data])
}
