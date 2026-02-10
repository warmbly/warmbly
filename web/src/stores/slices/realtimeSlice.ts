import type { StateCreator } from 'zustand'

export type ConnectionQuality = 'good' | 'degraded' | 'poor' | 'disconnected'

export interface RealtimeSlice {
  connectionStatus: 'connected' | 'connecting' | 'disconnected'
  reconnectAttempt: number
  connectionQuality: ConnectionQuality
  joinedChannels: string[]

  setConnectionStatus: (status: 'connected' | 'connecting' | 'disconnected') => void
  setReconnectAttempt: (attempt: number) => void
  setConnectionQuality: (quality: ConnectionQuality) => void
  addJoinedChannel: (channel: string) => void
  removeJoinedChannel: (channel: string) => void
  setJoinedChannels: (channels: string[]) => void
}

export const createRealtimeSlice: StateCreator<RealtimeSlice, [], [], RealtimeSlice> = (set) => ({
  connectionStatus: 'disconnected',
  reconnectAttempt: 0,
  connectionQuality: 'disconnected',
  joinedChannels: [],

  setConnectionStatus: (connectionStatus) => set({ connectionStatus }),
  setReconnectAttempt: (reconnectAttempt) => set({ reconnectAttempt }),
  setConnectionQuality: (connectionQuality) => set({ connectionQuality }),
  addJoinedChannel: (channel) =>
    set((state) => ({
      joinedChannels: state.joinedChannels.includes(channel)
        ? state.joinedChannels
        : [...state.joinedChannels, channel],
    })),
  removeJoinedChannel: (channel) =>
    set((state) => ({
      joinedChannels: state.joinedChannels.filter((c) => c !== channel),
    })),
  setJoinedChannels: (joinedChannels) => set({ joinedChannels }),
})
