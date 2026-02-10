import { ConversationList } from '@/components/app/unibox/ConversationList'
import { ThreadView } from '@/components/app/unibox/ThreadView'
import { useAppStore } from '@/stores'

export default function UniboxPage() {
  const selectedThreadId = useAppStore((s) => s.selectedThreadId)

  return (
    <div className="flex h-[calc(100vh-theme(spacing.14)-theme(spacing.8))] gap-0 -m-4">
      <div className="w-80 shrink-0 border-r-2 overflow-hidden flex flex-col">
        <ConversationList />
      </div>
      <div className="flex-1 overflow-hidden flex flex-col">
        {selectedThreadId ? (
          <ThreadView threadId={selectedThreadId} />
        ) : (
          <div className="flex-1 flex items-center justify-center text-muted-foreground">
            <div className="text-center">
              <p className="text-lg font-medium">Select a conversation</p>
              <p className="text-sm mt-1">Choose a thread from the left to view messages</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
