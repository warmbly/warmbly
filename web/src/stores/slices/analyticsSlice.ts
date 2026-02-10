import type { StateCreator } from 'zustand'
import type DashboardOverview from '@/lib/api/models/app/analytics/DashboardOverview'
import type WarmupAnalytics from '@/lib/api/models/app/analytics/WarmupAnalytics'
import type CampaignAnalytics from '@/lib/api/models/app/analytics/CampaignAnalytics'
import type UsageOverview from '@/lib/api/models/app/analytics/UsageOverview'

export interface AnalyticsSlice {
  dashboardOverview: DashboardOverview | null
  warmupAnalytics: WarmupAnalytics | null
  campaignAnalytics: Record<string, CampaignAnalytics>
  usageOverview: UsageOverview | null

  setDashboardOverview: (overview: DashboardOverview | null) => void
  setWarmupAnalytics: (analytics: WarmupAnalytics | null) => void
  setCampaignAnalytics: (id: string, analytics: CampaignAnalytics) => void
  setUsageOverview: (usage: UsageOverview | null) => void
}

export const createAnalyticsSlice: StateCreator<AnalyticsSlice, [], [], AnalyticsSlice> = (set) => ({
  dashboardOverview: null,
  warmupAnalytics: null,
  campaignAnalytics: {},
  usageOverview: null,

  setDashboardOverview: (dashboardOverview) => set({ dashboardOverview }),
  setWarmupAnalytics: (warmupAnalytics) => set({ warmupAnalytics }),
  setCampaignAnalytics: (id, analytics) =>
    set((state) => ({
      campaignAnalytics: { ...state.campaignAnalytics, [id]: analytics },
    })),
  setUsageOverview: (usageOverview) => set({ usageOverview }),
})
