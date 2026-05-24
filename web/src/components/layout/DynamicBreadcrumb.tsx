import { useLocation } from 'react-router-dom'
import { Fragment } from 'react'

const labelMap: Record<string, string> = {
  app: 'Dashboard',
  emails: 'Accounts',
  contacts: 'Contacts',
  campaigns: 'Campaigns',
  unibox: 'Inbox',
  analytics: 'Analytics',
  crm: 'CRM',
  pipelines: 'Pipelines',
  deals: 'Deals',
  tasks: 'Tasks',
  templates: 'Templates',
  'api-keys': 'API Keys',
  settings: 'Settings',
  billing: 'Billing',
  team: 'Team',
  leads: 'Leads',
  preferences: 'Preferences',
  schedule: 'Schedule',
  sequences: 'Sequences',
}

export function DynamicBreadcrumb() {
  const location = useLocation()
  const segments = location.pathname.split('/').filter(Boolean)
  const breadcrumbSegments = segments.filter((s) => s !== 'app')

  if (breadcrumbSegments.length === 0) {
    return (
      <div className="flex items-center gap-2 text-sm">
        <span className="font-medium text-zinc-900">Dashboard</span>
      </div>
    )
  }

  return (
    <div className="flex items-center gap-1.5 text-sm min-w-0">
      {breadcrumbSegments.map((segment, index) => {
        const isLast = index === breadcrumbSegments.length - 1
        const label = labelMap[segment] || segment.charAt(0).toUpperCase() + segment.slice(1)

        return (
          <Fragment key={segment + index}>
            {index > 0 && <span className="text-zinc-300">/</span>}
            {isLast ? (
              <span className="font-medium text-zinc-900 truncate">{label}</span>
            ) : (
              <span className="text-zinc-400 truncate">{label}</span>
            )}
          </Fragment>
        )
      })}
    </div>
  )
}
