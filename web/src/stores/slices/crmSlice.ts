import type { StateCreator } from 'zustand'
import type Pipeline from '@/lib/api/models/app/crm/Pipeline'
import type Deal from '@/lib/api/models/app/crm/Deal'
import type CRMTask from '@/lib/api/models/app/crm/CRMTask'

export interface CRMSlice {
  pipelines: Pipeline[]
  deals: Deal[]
  crmTasks: CRMTask[]

  setPipelines: (pipelines: Pipeline[]) => void
  addPipeline: (pipeline: Pipeline) => void
  updatePipeline: (id: string, updates: Partial<Pipeline>) => void
  removePipeline: (id: string) => void

  setDeals: (deals: Deal[]) => void
  addDeal: (deal: Deal) => void
  updateDeal: (id: string, updates: Partial<Deal>) => void
  removeDeal: (id: string) => void

  setCRMTasks: (tasks: CRMTask[]) => void
  addCRMTask: (task: CRMTask) => void
  updateCRMTask: (id: string, updates: Partial<CRMTask>) => void
  removeCRMTask: (id: string) => void
}

export const createCRMSlice: StateCreator<CRMSlice, [], [], CRMSlice> = (set) => ({
  pipelines: [],
  deals: [],
  crmTasks: [],

  setPipelines: (pipelines) => set({ pipelines }),
  addPipeline: (pipeline) => set((state) => ({ pipelines: [...state.pipelines, pipeline] })),
  updatePipeline: (id, updates) =>
    set((state) => ({
      pipelines: state.pipelines.map((p) => (p.id === id ? { ...p, ...updates } : p)),
    })),
  removePipeline: (id) => set((state) => ({ pipelines: state.pipelines.filter((p) => p.id !== id) })),

  setDeals: (deals) => set({ deals }),
  addDeal: (deal) => set((state) => ({ deals: [...state.deals, deal] })),
  updateDeal: (id, updates) =>
    set((state) => ({
      deals: state.deals.map((d) => (d.id === id ? { ...d, ...updates } : d)),
    })),
  removeDeal: (id) => set((state) => ({ deals: state.deals.filter((d) => d.id !== id) })),

  setCRMTasks: (crmTasks) => set({ crmTasks }),
  addCRMTask: (task) => set((state) => ({ crmTasks: [...state.crmTasks, task] })),
  updateCRMTask: (id, updates) =>
    set((state) => ({
      crmTasks: state.crmTasks.map((t) => (t.id === id ? { ...t, ...updates } : t)),
    })),
  removeCRMTask: (id) => set((state) => ({ crmTasks: state.crmTasks.filter((t) => t.id !== id) })),
})
