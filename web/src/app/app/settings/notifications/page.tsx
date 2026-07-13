import React from "react";
import {
    useNotificationPreferences,
    useUpdateNotificationPreferences,
} from "@/lib/api/hooks/app/notifications/useNotifications";
import type { NotificationCategoryKey, NotificationPreferences } from "@/lib/api/models/app/notifications/Notification";
import { Row, Section, SectionShell, Toggle } from "../_components/SectionShell";
import SaveStatus from "../_components/SaveStatus";
import { useAutosave } from "@/hooks/useAutosave";
import { useRegisterUnsaved } from "@/hooks/context/unsaved";

const INBOUND: { key: NotificationCategoryKey; label: string; hint: string }[] = [
    { key: "inbound_reply", label: "Reply received", hint: "A recipient replied to a cold email." },
    { key: "inbound_out_of_office", label: "Out-of-office detected", hint: "An auto-responder hit one of your sends." },
];

const HEALTH: { key: NotificationCategoryKey; label: string; hint: string }[] = [
    { key: "health_bounce", label: "Bounce detected", hint: "A campaign starts bouncing — notifies the campaign owner." },
    { key: "health_complaint", label: "Spam complaint", hint: "Any complaint event on one of your campaigns." },
    { key: "health_worker_downtime", label: "Worker downtime", hint: "A sender worker stops responding." },
];

const SECURITY: { key: NotificationCategoryKey; label: string; hint: string }[] = [
    { key: "security_new_signin", label: "New sign-in", hint: "Your account was accessed from a device you haven't used before." },
];

export default function NotificationsSettingsPage() {
    const { data, isLoading } = useNotificationPreferences();
    const update = useUpdateNotificationPreferences();
    const [draft, setDraft] = React.useState<NotificationPreferences | null>(null);

    // Auto-save: toggles persist instantly. markSaved on data load moves the
    // baseline to the server value so the initial null→data hydration (and any
    // refetch) is never mistaken for a user edit.
    const autosave = useAutosave({
        value: draft,
        enabled: !!draft,
        save: async (v) => {
            if (v) await update.mutateAsync(v);
        },
    });
    useRegisterUnsaved(autosave, () => setDraft(autosave.savedValue));

    React.useEffect(() => {
        if (data) {
            setDraft(data);
            autosave.markSaved(data);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [data]);

    const setEnabled = (key: NotificationCategoryKey, on: boolean) =>
        setDraft((d) => (d ? { ...d, [key]: { ...d[key], enabled: on } } : d));

    const CATEGORY_KEYS: NotificationCategoryKey[] = [
        "inbound_reply",
        "inbound_out_of_office",
        "health_bounce",
        "health_complaint",
        "health_worker_downtime",
        "security_new_signin",
    ];
    // Channels present globally: "on" when every category carries the channel.
    const channelOn = (ch: "email" | "slack" | "push") =>
        !!draft && CATEGORY_KEYS.every((k) => draft[k].channels[ch]);
    const setChannel = (ch: "email" | "slack" | "push", on: boolean) =>
        setDraft((d) => {
            if (!d) return d;
            const next = { ...d };
            for (const k of CATEGORY_KEYS) {
                next[k] = { ...d[k], channels: { ...d[k].channels, [ch]: on } };
            }
            return next;
        });

    const rows = (items: { key: NotificationCategoryKey; label: string; hint: string }[]) =>
        items.map((c) => (
            <Row key={c.key} label={c.label} description={c.hint}>
                <Toggle on={!!draft && draft[c.key].enabled} onChange={(v) => setEnabled(c.key, v)} />
            </Row>
        ));

    return (
        <SectionShell
            title="Notifications"
            description="Which events notify you, and where they are delivered. Defaults reflect the recommendation."
            actions={<SaveStatus status={autosave.status} onRetry={autosave.retry} />}
        >
            {isLoading || !draft ? (
                <div className="px-5 py-10 text-[12.5px] text-slate-400">Loading…</div>
            ) : (
                <>
                    <Section
                        eyebrow="Inbound activity"
                        description="Get notified about replies on a campaign you're running. Off by default to keep high-volume sends quiet."
                    >
                        {rows(INBOUND)}
                    </Section>
                    <Section eyebrow="Health" description="Deliverability + infrastructure alerts. Recommended on.">
                        {rows(HEALTH)}
                    </Section>
                    <Section eyebrow="Security" description="Account access alerts.">
                        {rows(SECURITY)}
                    </Section>
                    <Section eyebrow="Channels" description="Where enabled notifications are delivered. Applies across every category above.">
                        <Row label="In-app" description="The bell in the dashboard chrome (controlled per category above).">
                            <span className="text-[11px] font-medium text-emerald-600">On</span>
                        </Row>
                        <Row
                            label="Mobile push"
                            description="Alerts on devices signed in with the Warmbly iOS app. The first event pushes right away; bursts arrive as one summary instead of a ping per event."
                        >
                            <Toggle on={channelOn("push")} onChange={(v) => setChannel("push", v)} />
                        </Row>
                        <Row label="Email" description="Delivery to your account email.">
                            <Toggle on={channelOn("email")} onChange={(v) => setChannel("email", v)} />
                        </Row>
                        <Row label="Slack" description="Posts to your connected Slack, on the channel set up for Slack in the Integrations tab. Connect Slack and configure a channel there first.">
                            <Toggle on={channelOn("slack")} onChange={(v) => setChannel("slack", v)} />
                        </Row>
                    </Section>
                </>
            )}
        </SectionShell>
    );
}
