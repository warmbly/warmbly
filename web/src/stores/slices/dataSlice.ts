import type { StateCreator } from 'zustand'
import type Campaign from '@/lib/api/models/app/campaigns/Campaign'
import type Inbox from '@/lib/api/models/app/emails/Inbox'
import type Tag from '@/lib/api/models/app/Tag'
import type Folder from '@/lib/api/models/app/Folder'
import type Category from '@/lib/api/models/app/Category'

export interface DataSlice {
  // Cached data from API (synced from TanStack Query)
  campaigns: Campaign[]
  emails: Inbox[]
  tags: Tag[]
  folders: Folder[]
  categories: Category[]

  // Setters for syncing from TanStack Query
  setCampaigns: (campaigns: Campaign[]) => void
  setEmails: (emails: Inbox[]) => void
  setTags: (tags: Tag[]) => void
  setFolders: (folders: Folder[]) => void
  setCategories: (categories: Category[]) => void

  // CRUD helpers for optimistic updates
  addTag: (tag: Tag) => void
  updateTag: (id: string, updates: Partial<Tag>) => void
  removeTag: (id: string) => void

  addFolder: (folder: Folder) => void
  updateFolder: (id: string, updates: Partial<Folder>) => void
  removeFolder: (id: string) => void

  addCampaign: (campaign: Campaign) => void
  updateCampaign: (id: string, updates: Partial<Campaign>) => void
  removeCampaign: (id: string) => void

  addEmail: (email: Inbox) => void
  updateEmail: (id: string, updates: Partial<Inbox>) => void
  removeEmail: (id: string) => void
}

export const createDataSlice: StateCreator<DataSlice, [], [], DataSlice> = (set) => ({
  campaigns: [],
  emails: [],
  tags: [],
  folders: [],
  categories: [],

  setCampaigns: (campaigns) => set({ campaigns }),
  setEmails: (emails) => set({ emails }),
  setTags: (tags) => set({ tags }),
  setFolders: (folders) => set({ folders }),
  setCategories: (categories) => set({ categories }),

  // Tags CRUD
  addTag: (tag) => set((state) => ({ tags: [...state.tags, tag] })),
  updateTag: (id, updates) =>
    set((state) => ({
      tags: state.tags.map((t) => (t.id === id ? { ...t, ...updates } : t)),
    })),
  removeTag: (id) => set((state) => ({ tags: state.tags.filter((t) => t.id !== id) })),

  // Folders CRUD
  addFolder: (folder) => set((state) => ({ folders: [...state.folders, folder] })),
  updateFolder: (id, updates) =>
    set((state) => ({
      folders: state.folders.map((f) => (f.id === id ? { ...f, ...updates } : f)),
    })),
  removeFolder: (id) => set((state) => ({ folders: state.folders.filter((f) => f.id !== id) })),

  // Campaigns CRUD
  addCampaign: (campaign) => set((state) => ({ campaigns: [...state.campaigns, campaign] })),
  updateCampaign: (id, updates) =>
    set((state) => ({
      campaigns: state.campaigns.map((c) => (c.id === id ? { ...c, ...updates } : c)),
    })),
  removeCampaign: (id) =>
    set((state) => ({ campaigns: state.campaigns.filter((c) => c.id !== id) })),

  // Emails CRUD
  addEmail: (email) => set((state) => ({ emails: [...state.emails, email] })),
  updateEmail: (id, updates) =>
    set((state) => ({
      emails: state.emails.map((e) => (e.id === id ? { ...e, ...updates } : e)),
    })),
  removeEmail: (id) => set((state) => ({ emails: state.emails.filter((e) => e.id !== id) })),
})
