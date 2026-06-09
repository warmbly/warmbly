import React from "react";
import toast from "react-hot-toast";
import { TopbarAction } from "@/components/layout/Page";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import {
    useNotificationPreferences,
    useUpdateNotificationPreferences,
} from "@/lib/api/hooks/app/notifications/useNotifications";
import type { NotificationCategoryKey, NotificationPreferences } from "@/lib/api/models/app/notifications/Notification";
import { Row, Section, SectionShell, Toggle } from "../_components/SectionShell";

const INBOUND: { key: NotificationCategoryKey; label: string; hint: string }[] = [
    { key: "inbound_reply", label: "Reply received", hint: "A recipient replied to a cold email." },
    { key: "inbound_out_of_office", label: "Out-of-office detected", hint: "An auto-responder hit one of your sends." },
];

const HEALTH: { key: NotificationCategoryKey; label: string; hint: string }[] = [
    { key: "health_bounce", label: "Bounce detected", hint: "A campaign starts bouncing — notifies the campaign owner." },
    { key: "health_complaint", label: "Spam complaint", hint: "Any complaint event on one of your campaigns." },
    { key: "health_worker_downtime", label: "Worker downtime", hint: "A sender worker stops responding." },
];

export default function NotificationsSettingsPage() {
    const { data, isLoading } = useNotificationPreferences();
    const update = useUpdateNotificationPreferences();
    const [draft, setDraft] = React.useState<NotificationPreferences | null>(null);

    React.useEffect(() => {
        if (data) setDraft(data);
    }, [data]);

    const dirty = !!draft && !!data && JSON.stringify(draft) !== JSON.stringify(data);

    const setEnabled = (key: NotificationCategoryKey, on: boolean) =>
        setDraft((d) => (d ? { ...d, [key]: { ...d[key], enabled: on } } : d));

    const save = async () => {
        if (!draft || !dirty || update.isPending) return;
        try {
            await update.mutateAsync(draft);
            toast.success("Notification preferences saved");
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    };

    const rows = (items: { key: NotificationCategoryKey; label: string; hint: string }[]) =>
        items.map((c) => (
            <Row key={c.key} label={c.label} description={c.hint}>
                <Toggle on={!!draft && draft[c.key].enabled} onChange={(v) => setEnabled(c.key, v)} />
            </Row>
        ));

    return (
        <SectionShell
            title="Notifications"
            description="Which events show up in your in-app feed (the bell). Defaults reflect the recommendation."
            actions={
                dirty ? (
                    <>
                        <TopbarAction variant="ghost" onClick={() => data && setDraft(data)}>
                            Discard
                        </TopbarAction>
                        <TopbarAction onClick={save} disabled={update.isPending}>
                            {update.isPending ? "Saving…" : "Save"}
                        </TopbarAction>
                    </>
                ) : null
            }
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
                    <Section eyebrow="Channels" description="Where notifications are delivered.">
                        <Row label="In-app" description="The bell in the dashboard chrome (controlled per category above).">
                            <span className="text-[11px] font-medium text-emerald-600">On</span>
                        </Row>
                        <Row label="Email" description="Delivery to your account email.">
                            <span className="text-[11px] text-slate-400">Coming soon</span>
                        </Row>
                        <Row label="Slack" description="Connect via the Integrations tab.">
                            <span className="text-[11px] text-slate-400">Coming soon</span>
                        </Row>
                    </Section>
                </>
            )}
        </SectionShell>
    );
}
