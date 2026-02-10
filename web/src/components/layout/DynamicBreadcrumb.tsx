import { useLocation } from 'react-router-dom'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Fragment } from 'react'

const labelMap: Record<string, string> = {
  app: 'Dashboard',
  emails: 'Email Accounts',
  contacts: 'Contacts',
  campaigns: 'Campaigns',
  unibox: 'Unibox',
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

  // Remove "app" prefix for cleaner breadcrumbs
  const breadcrumbSegments = segments.filter((s) => s !== 'app')

  if (breadcrumbSegments.length === 0) {
    return (
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbPage>Dashboard</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
    )
  }

  return (
    <Breadcrumb>
      <BreadcrumbList>
        {breadcrumbSegments.map((segment, index) => {
          const isLast = index === breadcrumbSegments.length - 1
          const label = labelMap[segment] || segment.charAt(0).toUpperCase() + segment.slice(1)
          const path = '/app/' + segments.slice(1, segments.indexOf(segment) + 1).join('/')

          return (
            <Fragment key={segment + index}>
              {index > 0 && <BreadcrumbSeparator />}
              <BreadcrumbItem>
                {isLast ? (
                  <BreadcrumbPage>{label}</BreadcrumbPage>
                ) : (
                  <BreadcrumbLink href={path}>{label}</BreadcrumbLink>
                )}
              </BreadcrumbItem>
            </Fragment>
          )
        })}
      </BreadcrumbList>
    </Breadcrumb>
  )
}
