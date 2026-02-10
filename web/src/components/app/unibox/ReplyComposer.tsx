import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { SendIcon } from 'lucide-react'

interface ReplyComposerProps {
  threadId: string
}

export function ReplyComposer({ threadId: _threadId }: ReplyComposerProps) {
  const [reply, setReply] = useState('')

  const handleSend = () => {
    if (!reply.trim()) return
    // TODO: Implement send reply via API
    setReply('')
  }

  return (
    <div className="p-4 border-t-2">
      <textarea
        value={reply}
        onChange={(e) => setReply(e.target.value)}
        placeholder="Type your reply..."
        className="w-full min-h-[80px] border-2 border-input bg-transparent p-3 text-sm resize-none focus:outline-none focus:border-ring"
      />
      <div className="flex justify-end mt-2">
        <Button size="sm" onClick={handleSend} disabled={!reply.trim()}>
          <SendIcon className="size-3.5" />
          Send Reply
        </Button>
      </div>
    </div>
  )
}
