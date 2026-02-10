import { Link, useLocation } from 'react-router-dom'
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
  HelpCircleIcon,
  SettingsIcon,
  CreditCardIcon,
  Users2Icon,
} from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  SidebarSeparator,
} from '@/components/ui/sidebar'
import { OrgSwitcher } from './OrgSwitcher'
import { UserNav } from './UserNav'
import { ThemeToggle } from './ThemeToggle'
import { useAppStore } from '@/stores'
import { cn } from '@/lib/utils'

interface NavItem {
  title: string
  url: string
  icon: React.ComponentType<{ className?: string }>
  shortcut?: string
}

interface NavGroup {
  label: string
  items: NavItem[]
}

const mainNavItems: NavItem[] = [
  { title: 'Email Accounts', url: '/app/emails', icon: MailIcon, shortcut: 'g e' },
  { title: 'Contacts', url: '/app/contacts', icon: UsersIcon, shortcut: 'g c' },
  { title: 'Campaigns', url: '/app/campaigns', icon: MegaphoneIcon, shortcut: 'g m' },
  { title: 'Unibox', url: '/app/unibox', icon: InboxIcon, shortcut: 'g u' },
  { title: 'Analytics', url: '/app/analytics', icon: BarChart3Icon, shortcut: 'g a' },
]

const crmNavItems: NavItem[] = [
  { title: 'Pipelines', url: '/app/crm/pipelines', icon: GitBranchIcon, shortcut: 'g p' },
  { title: 'Deals', url: '/app/crm/deals', icon: CircleDollarSignIcon, shortcut: 'g d' },
  { title: 'Tasks', url: '/app/crm/tasks', icon: CheckSquareIcon, shortcut: 'g t' },
]

const resourceNavItems: NavItem[] = [
  { title: 'Templates', url: '/app/templates', icon: FileTextIcon, shortcut: 'g l' },
  { title: 'API Keys', url: '/app/api-keys', icon: KeyIcon, shortcut: 'g k' },
]

const bottomNavItems: NavItem[] = [
  { title: 'Settings', url: '/app/settings', icon: SettingsIcon, shortcut: 'g s' },
  { title: 'Billing', url: '/app/billing', icon: CreditCardIcon },
  { title: 'Team', url: '/app/team', icon: Users2Icon },
]

const navGroups: NavGroup[] = [
  { label: 'Main', items: mainNavItems },
  { label: 'CRM', items: crmNavItems },
  { label: 'Resources', items: resourceNavItems },
]

function ShortcutBadge({ shortcut }: { shortcut: string }) {
  return (
    <div className="ml-auto flex items-center gap-0.5 opacity-0 transition-opacity group-hover/menu-item:opacity-60">
      {shortcut.split(' ').map((key, i) => (
        <kbd
          key={i}
          className="inline-flex h-5 min-w-5 items-center justify-center border border-border bg-muted px-1 font-mono text-[10px] font-medium text-muted-foreground"
        >
          {key}
        </kbd>
      ))}
    </div>
  )
}

function NavItemComponent({ item }: { item: NavItem }) {
  const location = useLocation()
  const isActive = location.pathname === item.url || location.pathname.startsWith(item.url + '/')
  const unseenCount = useAppStore((s) => s.unseenCount)

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        asChild
        isActive={isActive}
        tooltip={item.title}
        className={cn(
          isActive && 'border-l-3 border-primary bg-sidebar-accent font-medium'
        )}
      >
        <Link to={item.url}>
          <item.icon className="size-4" />
          <span>{item.title}</span>
          {item.title === 'Unibox' && unseenCount > 0 && (
            <span className="ml-auto inline-flex h-5 min-w-5 items-center justify-center bg-primary px-1.5 text-[10px] font-bold text-primary-foreground">
              {unseenCount > 99 ? '99+' : unseenCount}
            </span>
          )}
          {item.shortcut && <ShortcutBadge shortcut={item.shortcut} />}
        </Link>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

export function AppSidebar() {
  const setShortcutsModalOpen = useAppStore((state) => state.setShortcutsModalOpen)

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <OrgSwitcher />
      </SidebarHeader>

      <SidebarContent>
        {navGroups.map((group) => (
          <SidebarGroup key={group.label}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => (
                  <NavItemComponent key={item.url} item={item} />
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}

        <SidebarSeparator className="mt-auto" />

        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {bottomNavItems.map((item) => (
                <NavItemComponent key={item.url} item={item} />
              ))}
              <SidebarMenuItem>
                <div className="flex items-center gap-2 px-2 py-1.5">
                  <ThemeToggle />
                  <span className="text-sm text-muted-foreground group-data-[collapsible=icon]:hidden">
                    Theme
                  </span>
                </div>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton onClick={() => setShortcutsModalOpen(true)}>
                  <HelpCircleIcon className="size-4" />
                  <span>Help</span>
                  <div className="ml-auto opacity-60 group-data-[collapsible=icon]:hidden">
                    <kbd className="inline-flex h-5 min-w-5 items-center justify-center border border-border bg-muted px-1 font-mono text-[10px] font-medium text-muted-foreground">
                      ?
                    </kbd>
                  </div>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <UserNav />
      </SidebarFooter>

      <SidebarRail />
    </Sidebar>
  )
}
