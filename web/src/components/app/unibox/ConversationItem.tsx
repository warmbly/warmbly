import type UniboxEmail from '@/lib/api/models/app/unibox/UniboxEmail'
import { useAppStore } from '@/stores'
import { cn } from '@/lib/utils'

interface ConversationItemProps {
  email: UniboxEmail
}

export function ConversationItem({ email }: ConversationItemProps) {
  const selectedThreadId = useAppStore((s) => s.selectedThreadId)
  const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId)

  const threadId = email.thread_id || email.id
  const isSelected = selectedThreadId === threadId
  const date = new Date(email.date)
  const timeStr = date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })

  return (
    <button
      onClick={() => setSelectedThreadId(threadId)}
      className={cn(
        'w-full text-left p-3 border-b border-border hover:bg-accent/50 transition-colors',
        isSelected && 'bg-accent border-l-3 border-l-primary',
        !email.is_seen && 'font-medium'
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <span className="text-sm truncate flex-1">{email.from}</span>
        <span className="text-xs text-muted-foreground shrink-0">{timeStr}</span>
      </div>
      <p className="text-sm truncate mt-0.5">{email.subject}</p>
      <p className="text-xs text-muted-foreground truncate mt-0.5">
        {email.body.replace(/<[^>]*>/g, '').slice(0, 80)}
      </p>
      {!email.is_seen && (
        <span className="inline-block size-2 bg-primary mt-1" />
      )}
    </button>
  )
}
