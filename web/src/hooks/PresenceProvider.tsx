import React, {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
} from 'react';
import { useLocation } from 'react-router-dom';
import { useSocket } from './context/socket';
import { useUserProfile } from './context/user';
import { useAppStore } from '@/stores';
import type { PresenceMeta } from '@/stores/slices/presenceSlice';

export type PresenceAction = 'viewing' | 'editing' | 'replying' | 'idle';

interface PresenceContextValue {
    /**
     * Claim (or release with null) the record this client is focused on.
     * Teammates watching the same record see it instantly.
     */
    setResource: (resource: string | null, action?: PresenceAction) => void;
}

const PresenceContext = createContext<PresenceContextValue>({
    setResource: () => {},
});

// Syncs org-channel presence into the store and pushes this client's
// activity (route + focused record) so teammates get the Discord-like
// "who's here / who's already on this" layer everywhere in the dashboard.
export function PresenceProvider({ children }: { children: React.ReactNode }) {
    const { isConnected, subscribeToChannel, pushToChannel } = useSocket();
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const setPresenceState = useAppStore((s) => s.setPresenceState);
    const applyPresenceDiff = useAppStore((s) => s.applyPresenceDiff);
    const clearPresence = useAppStore((s) => s.clearPresence);
    const { pathname } = useLocation();

    const activityRef = useRef<{ page: string | null; resource: string | null; action: string | null }>({
        page: null,
        resource: null,
        action: null,
    });
    const orgIdRef = useRef<string | null>(null);
    orgIdRef.current = currentOrg?.id ?? null;

    const pushActivity = useCallback(() => {
        const orgId = orgIdRef.current;
        if (!orgId) return;
        pushToChannel(`org:${orgId}`, 'presence:update', { ...activityRef.current });
    }, [pushToChannel]);

    // Track the current route. Changing pages implicitly drops any focused
    // record (detail panes unmount and clear themselves, but a hard
    // navigation must not leave a stale "viewing" claim behind).
    useEffect(() => {
        activityRef.current.page = pathname;
        pushActivity();
    }, [pathname, pushActivity]);

    // Presence sync for the active org channel. presence_state also doubles
    // as the "join completed" signal, so we re-push our current activity then
    // (covers reconnects, where the channel rejoins with empty meta).
    useEffect(() => {
        if (!isConnected || !currentOrg?.id) {
            clearPresence();
            return;
        }
        const topic = `org:${currentOrg.id}`;
        const offState = subscribeToChannel(topic, 'presence_state', (payload) => {
            setPresenceState(payload as Record<string, { metas: PresenceMeta[] }>);
            pushActivity();
        });
        const offDiff = subscribeToChannel(topic, 'presence_diff', (payload) => {
            applyPresenceDiff(payload as { joins?: Record<string, { metas: PresenceMeta[] }>; leaves?: Record<string, { metas: PresenceMeta[] }> });
        });
        return () => {
            offState();
            offDiff();
            clearPresence();
        };
    }, [isConnected, currentOrg?.id, subscribeToChannel, setPresenceState, applyPresenceDiff, clearPresence, pushActivity]);

    const setResource = useCallback(
        (resource: string | null, action: PresenceAction = 'viewing') => {
            activityRef.current.resource = resource;
            activityRef.current.action = resource ? action : null;
            pushActivity();
        },
        [pushActivity],
    );

    const value = useMemo(() => ({ setResource }), [setResource]);

    return <PresenceContext.Provider value={value}>{children}</PresenceContext.Provider>;
}

/**
 * Declare which record this component has focused while mounted, e.g.
 * `usePresenceResource(thread ? `thread:${thread}` : null)` in a detail pane
 * or `usePresenceResource(`automation:${id}`, 'editing')` in the builder.
 * Cleared automatically on unmount.
 */
export function usePresenceResource(resource: string | null, action: PresenceAction = 'viewing') {
    const { setResource } = useContext(PresenceContext);

    useEffect(() => {
        if (!resource) return;
        setResource(resource, action);
        return () => setResource(null);
    }, [resource, action, setResource]);
}

/** Imperative access (e.g. flip to "replying" while a composer is open). */
export function usePresenceActions() {
    return useContext(PresenceContext);
}

export interface PresenceUser {
    userId: string;
    name: string | null;
    avatar: string | null;
    page: string | null;
    resource: string | null;
    action: string | null;
}

const latestMeta = (metas: PresenceMeta[]): PresenceMeta =>
    metas.reduce((a, b) => ((b.updated_at ?? 0) >= (a.updated_at ?? 0) ? b : a), metas[0] ?? {});

const toUser = (userId: string, metas: PresenceMeta[]): PresenceUser => {
    const meta = latestMeta(metas);
    return {
        userId,
        name: meta.name ?? null,
        avatar: meta.avatar ?? null,
        page: meta.page ?? null,
        resource: meta.resource ?? null,
        action: meta.action ?? null,
    };
};

/** Every org member currently online, excluding this client's own user. */
export function useOnlineMembers(): PresenceUser[] {
    const presence = useAppStore((s) => s.presence);
    const { user } = useUserProfile();

    return useMemo(
        () =>
            Object.entries(presence)
                .filter(([userId, metas]) => userId !== user?.id && metas.length > 0)
                .map(([userId, metas]) => toUser(userId, metas))
                .sort((a, b) => (a.name ?? '').localeCompare(b.name ?? '')),
        [presence, user?.id],
    );
}

/**
 * Teammates focused on the given record right now (any of their sockets),
 * excluding this client's own user. Powers "X is viewing / already replying".
 */
export function useResourceViewers(resource: string | null): PresenceUser[] {
    const presence = useAppStore((s) => s.presence);
    const { user } = useUserProfile();

    return useMemo(() => {
        if (!resource) return [];
        return Object.entries(presence)
            .filter(([userId]) => userId !== user?.id)
            .map(([userId, metas]) => {
                const focused = metas.filter((m) => m.resource === resource);
                return focused.length ? toUser(userId, focused) : null;
            })
            .filter((u): u is PresenceUser => u !== null);
    }, [presence, resource, user?.id]);
}
