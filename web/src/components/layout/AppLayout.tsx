import { Outlet } from 'react-router-dom'
import { SidebarProvider, SidebarInset, SidebarTrigger } from '@/components/ui/sidebar'
import { Separator } from '@/components/ui/separator'
import { AppSidebar } from './AppSidebar'
import { ThemeProvider } from './ThemeProvider'
import { useAppStore } from '@/stores'
import { useKeyboardShortcuts } from '@/hooks/useKeyboardShortcuts'
import { ShortcutsModal } from '@/components/shared/ShortcutsModal'
import { CommandPalette } from '@/components/shared/CommandPalette'
import { DynamicBreadcrumb } from './DynamicBreadcrumb'
import { ConnectionIndicator } from '@/components/shared/ConnectionIndicator'
import { SearchIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'

export function AppLayout() {
  const sidebarCollapsed = useAppStore((state) => state.sidebarCollapsed)
  const setCommandPaletteOpen = useAppStore((state) => state.setCommandPaletteOpen)

  // Initialize keyboard shortcuts
  useKeyboardShortcuts()

  return (
    <ThemeProvider>
      <SidebarProvider defaultOpen={!sidebarCollapsed}>
        <AppSidebar />
        <SidebarInset>
          <header className="flex h-14 shrink-0 items-center gap-2 border-b-2 px-4">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 h-4" />
            <DynamicBreadcrumb />
            <div className="ml-auto flex items-center gap-3">
              <ConnectionIndicator />
              <Button
                variant="outline"
                size="sm"
                className="hidden sm:flex items-center gap-2 text-muted-foreground"
                onClick={() => setCommandPaletteOpen(true)}
              >
                <SearchIcon className="size-3.5" />
                <span className="text-xs">Search...</span>
                <kbd className="pointer-events-none ml-2 inline-flex h-5 items-center border border-border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground">
                  Ctrl+K
                </kbd>
              </Button>
              <Button
                variant="outline"
                size="icon-sm"
                className="sm:hidden"
                onClick={() => setCommandPaletteOpen(true)}
              >
                <SearchIcon className="size-4" />
              </Button>
            </div>
          </header>
          <main className="flex-1 overflow-auto p-4">
            <Outlet />
          </main>
        </SidebarInset>
      </SidebarProvider>

      <ShortcutsModal />
      <CommandPalette />
    </ThemeProvider>
  )
}
