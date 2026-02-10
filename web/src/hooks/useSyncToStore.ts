import { useEffect, useRef } from 'react'

export function useSyncToStore<T>(data: T | undefined, syncFn: (data: T) => void) {
  const syncFnRef = useRef(syncFn)
  syncFnRef.current = syncFn

  useEffect(() => {
    if (data !== undefined) {
      syncFnRef.current(data)
    }
  }, [data])
}
