import { useNavigate } from '@tanstack/react-router'
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
  WalletIcon,
  CheckIcon,
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
import { THEME_OPTIONS } from '@/components/app/theme/themeOptions'

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
  const setShortcutsModalOpen = useAppStore((state) => state.setShortcutsModalOpen)
  const theme = useAppStore((state) => state.theme)
  const setTheme = useAppStore((state) => state.setTheme)

  const runCommand = (command: () => void) => {
    setOpen(false)
    command()
  }

  const navigationCommands: CommandItem[] = [
    { icon: MailIcon, label: 'Email Accounts', shortcut: 'g e', onSelect: () => navigate({ to: '/app/emails' }) },
    { icon: UsersIcon, label: 'Contacts', shortcut: 'g c', onSelect: () => navigate({ to: '/app/contacts' }) },
    { icon: MegaphoneIcon, label: 'Campaigns', shortcut: 'g m', onSelect: () => navigate({ to: '/app/campaigns' }) },
    { icon: InboxIcon, label: 'Unibox', shortcut: 'g u', onSelect: () => navigate({ to: '/app/unibox/{-$scope}/{-$threadId}', params: { scope: undefined, threadId: undefined } }) },
    { icon: BarChart3Icon, label: 'Analytics', shortcut: 'g a', onSelect: () => navigate({ to: '/app/analytics' }) },
    { icon: GitBranchIcon, label: 'Pipelines', shortcut: 'g p', onSelect: () => navigate({ to: '/app/crm/pipelines' }) },
    { icon: CircleDollarSignIcon, label: 'Deals', shortcut: 'g d', onSelect: () => navigate({ to: '/app/crm/deals' }) },
    { icon: CheckSquareIcon, label: 'Tasks', shortcut: 'g t', onSelect: () => navigate({ to: '/app/crm/tasks' }) },
    { icon: FileTextIcon, label: 'Templates', shortcut: 'g l', onSelect: () => navigate({ to: '/app/templates' }) },
    { icon: KeyIcon, label: 'API Keys', shortcut: 'g k', onSelect: () => navigate({ to: '/app/api-keys' }) },
    { icon: SettingsIcon, label: 'Settings', shortcut: 'g s', onSelect: () => navigate({ to: '/app/settings' }) },
    { icon: CreditCardIcon, label: 'Billing', onSelect: () => navigate({ to: '/app/settings/billing/{-$tab}', params: { tab: undefined } }) },
    { icon: WalletIcon, label: 'Add money', onSelect: () => navigate({ to: '/app/settings/billing/{-$tab}', params: { tab: 'balance' } }) },
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

        <CommandGroup heading="Actions">
          <CommandItem onSelect={() => runCommand(() => setShortcutsModalOpen(true))}>
            <KeyIcon className="mr-2 size-4" />
            <span>Keyboard shortcuts</span>
            <span className="ml-auto text-xs text-muted-foreground">?</span>
          </CommandItem>
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Theme">
          {THEME_OPTIONS.map(({ value, label, icon: Icon }) => (
            <CommandItem
              key={value}
              value={`theme ${label} ${value === 'dark' ? 'dark mode night' : value === 'light' ? 'light mode day' : 'system auto device'}`}
              onSelect={() => runCommand(() => setTheme(value))}
            >
              <Icon className="mr-2 size-4" />
              <span>{value === 'system' ? 'Match system theme' : `${label} theme`}</span>
              {theme === value && <CheckIcon className="ml-auto size-3.5 text-muted-foreground" />}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
