import type {
    AppNotification,
    NotificationPreferences,
    NotificationPreferencesEnvelope,
} from "@/lib/api/models/app/notifications/Notification";
import Request from "../../Request";

export async function getNotificationPreferences(): Promise<NotificationPreferencesEnvelope> {
    return await Request<NotificationPreferencesEnvelope>({
        method: "GET",
        url: "/auth/me/notification-preferences",
        authorization: true,
    });
}

export async function updateNotificationPreferences(preferences: NotificationPreferences): Promise<NotificationPreferences> {
    const res = await Request<{ preferences: NotificationPreferences }>({
        method: "PUT",
        url: "/auth/me/notification-preferences",
        data: { preferences },
        authorization: true,
    });
    return res.preferences;
}

export async function listNotifications(unreadOnly = false, limit = 50): Promise<{ notifications: AppNotification[]; unread: number }> {
    const params = new URLSearchParams();
    if (unreadOnly) params.set("unread", "1");
    params.set("limit", String(limit));
    return await Request<{ notifications: AppNotification[]; unread: number }>({
        method: "GET",
        url: `/auth/me/notifications?${params.toString()}`,
        authorization: true,
    });
}

export async function markNotificationRead(id: string): Promise<void> {
    await Request<void>({
        method: "POST",
        url: `/auth/me/notifications/${id}/read`,
        authorization: true,
    });
}

export async function markAllNotificationsRead(): Promise<void> {
    await Request<void>({
        method: "PUT",
        url: "/auth/me/notifications",
        authorization: true,
    });
}
