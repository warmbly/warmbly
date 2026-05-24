import type { StateCreator } from 'zustand'
import type Subscription from '@/lib/api/models/app/subscription/Subscription'
import type SubscriptionLimits from '@/lib/api/models/app/subscription/SubscriptionLimits'
import type TrialStatus from '@/lib/api/models/app/subscription/TrialStatus'
import type FeatureStatus from '@/lib/api/models/app/subscription/FeatureStatus'
import type Plan from '@/lib/api/models/app/subscription/Plan'

export interface SubscriptionSlice {
  subscription: Subscription | null
  subscriptionLimits: SubscriptionLimits | null
  trialStatus: TrialStatus | null
  featureStatus: FeatureStatus | null
  plans: Plan[]

  setSubscription: (subscription: Subscription | null) => void
  setSubscriptionLimits: (limits: SubscriptionLimits | null) => void
  setTrialStatus: (status: TrialStatus | null) => void
  setFeatureStatus: (status: FeatureStatus | null) => void
  setPlans: (plans: Plan[]) => void
}

export const createSubscriptionSlice: StateCreator<SubscriptionSlice, [], [], SubscriptionSlice> = (set) => ({
  subscription: null,
  subscriptionLimits: null,
  trialStatus: null,
  featureStatus: null,
  plans: [],

  setSubscription: (subscription) => set({ subscription }),
  setSubscriptionLimits: (subscriptionLimits) => set({ subscriptionLimits }),
  setTrialStatus: (trialStatus) => set({ trialStatus }),
  setFeatureStatus: (featureStatus) => set({ featureStatus }),
  setPlans: (plans) => set({ plans }),
})
