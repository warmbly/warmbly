import { useNavigate } from 'react-router-dom'
import {
  MailIcon,
  UsersIcon,
  MegaphoneIcon,
  InboxIcon,
  BarChart3Icon,
  GitBranchIcon,
  CircleDollarSignIcon,
  CheckSquareIcon,
  FileTextIcon,
  KeyIcon,
  SettingsIcon,
  CreditCardIcon,
  MoonIcon,
  SunIcon,
  MonitorIcon,
} from 'lucide-react'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { useAppStore } from '@/stores'

interface CommandItem {
  icon: React.ComponentType<{ className?: string }>
  label: string
  shortcut?: string
  onSelect: () => void
}

export function CommandPalette() {
  const navigate = useNavigate()
  const open = useAppStore((state) => state.commandPaletteOpen)
  const setOpen = useAppStore((state) => state.setCommandPaletteOpen)
  const setTheme = useAppStore((state) => state.setTheme)
  const setShortcutsModalOpen = useAppStore((state) => state.setShortcutsModalOpen)

  const runCommand = (command: () => void) => {
    setOpen(false)
    command()
  }

  const navigationCommands: CommandItem[] = [
    { icon: MailIcon, label: 'Email Accounts', shortcut: 'g e', onSelect: () => navigate('/app/emails') },
    { icon: UsersIcon, label: 'Contacts', shortcut: 'g c', onSelect: () => navigate('/app/contacts') },
    { icon: MegaphoneIcon, label: 'Campaigns', shortcut: 'g m', onSelect: () => navigate('/app/campaigns') },
    { icon: InboxIcon, label: 'Unibox', shortcut: 'g u', onSelect: () => navigate('/app/unibox') },
    { icon: BarChart3Icon, label: 'Analytics', shortcut: 'g a', onSelect: () => navigate('/app/analytics') },
    { icon: GitBranchIcon, label: 'Pipelines', shortcut: 'g p', onSelect: () => navigate('/app/crm/pipelines') },
    { icon: CircleDollarSignIcon, label: 'Deals', shortcut: 'g d', onSelect: () => navigate('/app/crm/deals') },
    { icon: CheckSquareIcon, label: 'Tasks', shortcut: 'g t', onSelect: () => navigate('/app/crm/tasks') },
    { icon: FileTextIcon, label: 'Templates', shortcut: 'g l', onSelect: () => navigate('/app/templates') },
    { icon: KeyIcon, label: 'API Keys', shortcut: 'g k', onSelect: () => navigate('/app/api-keys') },
    { icon: SettingsIcon, label: 'Settings', shortcut: 'g s', onSelect: () => navigate('/app/settings') },
    { icon: CreditCardIcon, label: 'Billing', onSelect: () => navigate('/app/settings/billing') },
  ]

  const themeCommands: CommandItem[] = [
    { icon: SunIcon, label: 'Light theme', onSelect: () => setTheme('light') },
    { icon: MoonIcon, label: 'Dark theme', onSelect: () => setTheme('dark') },
    { icon: MonitorIcon, label: 'System theme', onSelect: () => setTheme('system') },
  ]

  return (
    <CommandDialog
      open={open}
      onOpenChange={setOpen}
      // Anchor near the top on phones so the soft keyboard (opened by the
      // autofocused input) doesn't cover the results; md+ keeps the exact
      // default centered placement.
      className="top-[10%] translate-y-0 md:top-[50%] md:translate-y-[-50%]"
    >
      <CommandInput placeholder="Type a command or search..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>

        <CommandGroup heading="Navigation">
          {navigationCommands.map((command) => (
            <CommandItem
              key={command.label}
              onSelect={() => runCommand(command.onSelect)}
            >
              <command.icon className="mr-2 size-4" />
              <span>{command.label}</span>
              {command.shortcut && (
                <span className="ml-auto text-xs text-muted-foreground">
                  {command.shortcut}
                </span>
              )}
            </CommandItem>
          ))}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Theme">
          {themeCommands.map((command) => (
            <CommandItem
              key={command.label}
              onSelect={() => runCommand(command.onSelect)}
            >
              <command.icon className="mr-2 size-4" />
              <span>{command.label}</span>
            </CommandItem>
          ))}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Actions">
          <CommandItem onSelect={() => runCommand(() => setShortcutsModalOpen(true))}>
            <KeyIcon className="mr-2 size-4" />
            <span>Keyboard shortcuts</span>
            <span className="ml-auto text-xs text-muted-foreground">?</span>
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
