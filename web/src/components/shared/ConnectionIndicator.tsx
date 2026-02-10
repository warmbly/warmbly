import { useConnectionStatus } from '@/stores'
import { cn } from '@/lib/utils'

export function ConnectionIndicator() {
  const { status, quality } = useConnectionStatus()

  if (status === 'connected' && quality === 'good') {
    return null
  }

  return (
    <div className="flex items-center gap-2">
      <span
        className={cn(
          'size-2.5 border border-foreground/20',
          status === 'connected' && quality === 'degraded' && 'bg-yellow-500 animate-pulse',
          status === 'connected' && quality === 'poor' && 'bg-red-500',
          status === 'connecting' && 'bg-yellow-500 animate-pulse',
          status === 'disconnected' && 'bg-red-500',
        )}
      />
      <span className="text-xs text-muted-foreground hidden sm:inline">
        {status === 'connecting' && 'Reconnecting...'}
        {status === 'disconnected' && 'Disconnected'}
        {status === 'connected' && quality === 'degraded' && 'Slow connection'}
        {status === 'connected' && quality === 'poor' && 'Poor connection'}
      </span>
    </div>
  )
}
