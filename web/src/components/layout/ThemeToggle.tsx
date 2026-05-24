import { MoonIcon, SunIcon, MonitorIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useAppStore } from '@/stores'
import type { Theme } from '@/stores/slices/uiSlice'

export function ThemeToggle() {
  const theme = useAppStore((state) => state.theme)
  const resolvedTheme = useAppStore((state) => state.resolvedTheme)
  const setTheme = useAppStore((state) => state.setTheme)

  const themes: { value: Theme; label: string; icon: React.ReactNode }[] = [
    { value: 'light', label: 'Light', icon: <SunIcon className="size-4" /> },
    { value: 'dark', label: 'Dark', icon: <MoonIcon className="size-4" /> },
    { value: 'system', label: 'System', icon: <MonitorIcon className="size-4" /> },
  ]

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-8">
          {resolvedTheme === 'dark' ? (
            <MoonIcon className="size-4" />
          ) : (
            <SunIcon className="size-4" />
          )}
          <span className="sr-only">Toggle theme</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {themes.map((t) => (
          <DropdownMenuItem
            key={t.value}
            onClick={() => setTheme(t.value)}
            className={theme === t.value ? 'bg-accent' : ''}
          >
            {t.icon}
            <span className="ml-2">{t.label}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
