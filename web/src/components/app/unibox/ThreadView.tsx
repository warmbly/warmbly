import { MessageBubble } from './MessageBubble'
import { ReplyComposer } from './ReplyComposer'
import { useAppStore } from '@/stores'

interface ThreadViewProps {
  threadId: string
}

export function ThreadView({ threadId }: ThreadViewProps) {
  const emails = useAppStore((s) => s.uniboxEmails)
  const threadEmails = emails.filter(
    (e) => e.thread_id === threadId || e.id === threadId
  )

  if (threadEmails.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground">
        <p>Loading thread...</p>
      </div>
    )
  }

  const subject = threadEmails[0]?.subject || 'No Subject'

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b-2">
        <h2 className="text-lg font-semibold">{subject}</h2>
        <p className="text-sm text-muted-foreground">
          {threadEmails.length} message{threadEmails.length !== 1 ? 's' : ''}
        </p>
      </div>
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {threadEmails.map((email) => (
          <MessageBubble key={email.id} email={email} />
        ))}
      </div>
      <ReplyComposer threadId={threadId} />
    </div>
  )
}
