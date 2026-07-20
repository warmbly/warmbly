import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    getNotificationPreferences,
    updateNotificationPreferences,
    listNotifications,
    markNotificationRead,
    markAllNotificationsRead,
} from "@/lib/api/client/app/notifications/notifications";
import type { NotificationPreferences } from "@/lib/api/models/app/notifications/Notification";

const PREFS_KEY = ["notifications", "preferences"];
const FEED_KEY = ["notifications", "feed"];

export function useNotificationPreferences() {
    return useQuery({
        queryKey: PREFS_KEY,
        queryFn: getNotificationPreferences,
        staleTime: 60_000,
    });
}

// The PUT echoes the saved envelope; write it into the cache instead of
// invalidating — a refetch per save is wasted traffic and re-rendered the
// settings page mid-save.
export function useUpdateNotificationPreferences() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (prefs: NotificationPreferences) => updateNotificationPreferences(prefs),
        onSuccess: (envelope) => qc.setQueryData(PREFS_KEY, envelope),
    });
}

export function useNotifications() {
    return useQuery({
        queryKey: FEED_KEY,
        queryFn: () => listNotifications(false, 50),
        staleTime: 15_000,
    });
}

export function useMarkNotificationRead() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => markNotificationRead(id),
        onSuccess: () => qc.invalidateQueries({ queryKey: FEED_KEY }),
    });
}

export function useMarkAllNotificationsRead() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => markAllNotificationsRead(),
        onSuccess: () => qc.invalidateQueries({ queryKey: FEED_KEY }),
    });
}
