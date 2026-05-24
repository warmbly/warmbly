import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useAppStore } from '@/stores'
import { shortcutDefinitions } from '@/hooks/useKeyboardShortcuts'

function KeyboardKey({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex h-6 min-w-6 items-center justify-center rounded border border-border bg-muted px-1.5 font-mono text-xs font-medium text-muted-foreground">
      {children}
    </kbd>
  )
}

function ShortcutRow({ keys, description }: { keys: string[]; description: string }) {
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

function ShortcutGroup({
  title,
  shortcuts,
}: {
  title: string
  shortcuts: { keys: string[]; description: string }[]
}) {
  return (
    <div className="space-y-1">
      <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
        {title}
      </h3>
      <div className="divide-y divide-border">
        {shortcuts.map((shortcut, i) => (
          <ShortcutRow key={i} keys={shortcut.keys} description={shortcut.description} />
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
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Keyboard Shortcuts</DialogTitle>
        </DialogHeader>

        <div className="grid gap-6 md:grid-cols-2">
          <div className="space-y-6">
            <ShortcutGroup title="Navigation" shortcuts={shortcutDefinitions.navigation} />
          </div>
          <div className="space-y-6">
            <ShortcutGroup title="List Navigation" shortcuts={shortcutDefinitions.list} />
            <ShortcutGroup title="Actions" shortcuts={shortcutDefinitions.actions} />
          </div>
        </div>

        <div className="mt-4 text-center text-sm text-muted-foreground">
          Press <KeyboardKey>?</KeyboardKey> anytime to show this dialog
        </div>
      </DialogContent>
    </Dialog>
  )
}
