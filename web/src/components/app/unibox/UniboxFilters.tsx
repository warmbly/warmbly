import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface UniboxFiltersProps {
  value: 'all' | 'unread'
  onChange: (value: 'all' | 'unread') => void
}

export function UniboxFilters({ value, onChange }: UniboxFiltersProps) {
  return (
    <div className="flex gap-1">
      <Button
        variant={value === 'all' ? 'secondary' : 'ghost'}
        size="xs"
        onClick={() => onChange('all')}
        className={cn(value === 'all' && 'font-medium')}
      >
        All
      </Button>
      <Button
        variant={value === 'unread' ? 'secondary' : 'ghost'}
        size="xs"
        onClick={() => onChange('unread')}
        className={cn(value === 'unread' && 'font-medium')}
      >
        Unread
      </Button>
    </div>
  )
}
