import { useState } from 'react'
import { Input } from '@/components/ui/input'
import { ConversationItem } from './ConversationItem'
import { UniboxFilters } from './UniboxFilters'
import { useAppStore } from '@/stores'
import { SearchIcon } from 'lucide-react'

export function ConversationList() {
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState<'all' | 'unread'>('all')
  const emails = useAppStore((s) => s.uniboxEmails)

  const filtered = emails.filter((email) => {
    if (filter === 'unread' && email.is_seen) return false
    if (search) {
      const q = search.toLowerCase()
      return (
        email.subject.toLowerCase().includes(q) ||
        email.from.toLowerCase().includes(q)
      )
    }
    return true
  })

  return (
    <>
      <div className="p-3 border-b-2 space-y-2">
        <div className="relative">
          <SearchIcon className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
          <Input
            placeholder="Search conversations..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-8 h-8 text-sm"
          />
        </div>
        <UniboxFilters value={filter} onChange={setFilter} />
      </div>
      <div className="flex-1 overflow-y-auto">
        {filtered.length === 0 ? (
          <div className="p-4 text-center text-sm text-muted-foreground">
            No conversations found
          </div>
        ) : (
          filtered.map((email) => (
            <ConversationItem key={email.id} email={email} />
          ))
        )}
      </div>
    </>
  )
}
