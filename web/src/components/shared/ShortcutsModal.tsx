import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useAppStore } from '@/stores'
import {
  shortcutGroupTitles,
  visibleShortcuts,
  type ShortcutGroupId,
  type ShortcutRow,
} from '@/hooks/useKeyboardShortcuts'

function KeyboardKey({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex h-6 min-w-6 items-center justify-center rounded border border-border bg-muted px-1.5 font-mono text-xs font-medium text-muted-foreground">
      {children}
    </kbd>
  )
}

function ShortcutRowView({ keys, description }: ShortcutRow) {
  return (
    <div className="flex items-center justify-between py-1.5">
      <span className="text-sm text-foreground">{description}</span>
      <div className="flex items-center gap-1">
        {keys.map((key, i) => (
          <span key={i} className="flex items-center gap-1">
            <KeyboardKey>{key}</KeyboardKey>
            {i < keys.length - 1 && <span className="text-muted-foreground">+</span>}
          </span>
        ))}
      </div>
    </div>
  )
}

// A group with nothing live on this screen renders nothing. Every row below is
// one the dispatcher will actually run right now.
function ShortcutGroup({ group }: { group: ShortcutGroupId }) {
  const rows = visibleShortcuts(group)
  if (rows.length === 0) return null

  return (
    <div className="space-y-1">
      <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
        {shortcutGroupTitles[group]}
      </h3>
      <div className="divide-y divide-border">
        {rows.map((row, i) => (
          <ShortcutRowView key={i} {...row} />
        ))}
      </div>
    </div>
  )
}

export function ShortcutsModal() {
  const open = useAppStore((state) => state.shortcutsModalOpen)
  const setOpen = useAppStore((state) => state.setShortcutsModalOpen)

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="sm:max-w-2xl max-h-[80dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Keyboard Shortcuts</DialogTitle>
        </DialogHeader>

        <div className="grid gap-6 md:grid-cols-2">
          <div className="space-y-6">
            <ShortcutGroup group="navigation" />
          </div>
          <div className="space-y-6">
            <ShortcutGroup group="list" />
            <ShortcutGroup group="actions" />
            <ShortcutGroup group="assistant" />
          </div>
        </div>

        <div className="mt-4 text-center text-sm text-muted-foreground">
          Press <KeyboardKey>?</KeyboardKey> anytime to show this dialog
        </div>
      </DialogContent>
    </Dialog>
  )
}
