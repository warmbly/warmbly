import type UniboxEmail from '@/lib/api/models/app/unibox/UniboxEmail'
import { Card, CardContent } from '@/components/ui/card'

interface MessageBubbleProps {
  email: UniboxEmail
}

export function MessageBubble({ email }: MessageBubbleProps) {
  const date = new Date(email.date)
  const timeStr = date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })

  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center justify-between mb-2">
          <div>
            <span className="text-sm font-medium">{email.from}</span>
            <span className="text-xs text-muted-foreground ml-2">to {email.to}</span>
          </div>
          <span className="text-xs text-muted-foreground">{timeStr}</span>
        </div>
        <div
          className="text-sm prose prose-sm max-w-none"
          dangerouslySetInnerHTML={{ __html: email.body }}
        />
      </CardContent>
    </Card>
  )
}
