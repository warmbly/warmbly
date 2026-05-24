import type { StateCreator } from 'zustand'

export type ConnectionQuality = 'good' | 'degraded' | 'poor' | 'disconnected'

export interface RealtimeSlice {
  connectionStatus: 'connected' | 'connecting' | 'disconnected'
  reconnectAttempt: number
  connectionQuality: ConnectionQuality
  joinedChannels: string[]
  /**
   * Roundtrip latency to the realtime server in milliseconds. null
   * until the first heartbeat reply lands. Updated on every heartbeat
   * (~30s) so the LivePanel can surface a real "good / degraded /
   * poor" health signal instead of guessing from connectionStatus.
   */
  wsLatencyMs: number | null

  setConnectionStatus: (status: 'connected' | 'connecting' | 'disconnected') => void
  setReconnectAttempt: (attempt: number) => void
  setConnectionQuality: (quality: ConnectionQuality) => void
  addJoinedChannel: (channel: string) => void
  removeJoinedChannel: (channel: string) => void
  setJoinedChannels: (channels: string[]) => void
  setWsLatencyMs: (ms: number | null) => void
}

export const createRealtimeSlice: StateCreator<RealtimeSlice, [], [], RealtimeSlice> = (set) => ({
  connectionStatus: 'disconnected',
  reconnectAttempt: 0,
  connectionQuality: 'disconnected',
  joinedChannels: [],
  wsLatencyMs: null,
  setWsLatencyMs: (ms) => set({ wsLatencyMs: ms }),

  setConnectionStatus: (connectionStatus) =>
    set((state) => (state.connectionStatus === connectionStatus ? state : { connectionStatus })),
  setReconnectAttempt: (reconnectAttempt) =>
    set((state) => (state.reconnectAttempt === reconnectAttempt ? state : { reconnectAttempt })),
  setConnectionQuality: (connectionQuality) =>
    set((state) => (state.connectionQuality === connectionQuality ? state : { connectionQuality })),
  addJoinedChannel: (channel) =>
    set((state) => ({
      joinedChannels: state.joinedChannels.includes(channel)
        ? state.joinedChannels
        : [...state.joinedChannels, channel],
    })),
  removeJoinedChannel: (channel) =>
    set((state) => {
      const next = state.joinedChannels.filter((c) => c !== channel)
      return next.length === state.joinedChannels.length ? state : { joinedChannels: next }
    }),
  setJoinedChannels: (joinedChannels) =>
    set((state) => {
      if (
        state.joinedChannels.length === joinedChannels.length &&
        state.joinedChannels.every((c, i) => c === joinedChannels[i])
      ) {
        return state
      }
      return { joinedChannels }
    }),
})
