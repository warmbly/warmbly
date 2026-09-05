// Operator notification channels: where this deployment tells its operator
// that something happened. Discord, Slack, a generic signed webhook, or email.
//
// The channels live in the same instance settings document as the rest of the
// writable configuration, so this page reads and writes /admin/instance/settings
// and only reaches for its own endpoints for the event catalog and the test
// delivery probe.
//
// Targets and secrets come back redacted. An unchanged field is sent back as
// the preview the server returned (or empty) and resolves to the stored value,
// so saving an unrelated toggle can never wipe a credential.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    Bell,
    Check,
    Hash,
    Mail,
    Plus,
    Save,
    Send,
    Trash2,
    Webhook,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { ErrorState } from "@/components/ErrorState";
import { Button } from "@/components/ui/button";
import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
    getInstanceSettings,
    getNotificationEvents,
    putInstanceSettings,
    testNotificationChannel,
    type InstanceSettings,
    type NotifyChannel,
    type NotifyChannelType,
    type NotifyEventDef,
} from "@/lib/api/client/admin/instance";

const SETTINGS_KEY = ["admin", "instance", "settings"];
const EVENTS_KEY = ["admin", "instance", "notification-events"];

const TYPES: {
    value: NotifyChannelType;
    label: string;
    icon: typeof Hash;
    placeholder: string;
    help: string;
}[] = [
    {
        value: "discord",
        label: "Discord",
        icon: Hash,
        placeholder: "https://discord.com/api/webhooks/…",
        help: "Server settings, then Integrations, then New Webhook. Copy the webhook URL.",
    },
    {
        value: "slack",
        label: "Slack",
        icon: Hash,
        placeholder: "https://hooks.slack.com/services/…",
        help: "Create a Slack app with an incoming webhook and copy its URL.",
    },
    {
        value: "webhook",
        label: "Webhook",
        icon: Webhook,
        placeholder: "https://example.com/hooks/warmbly",
        help: "Receives the event as JSON. Set a secret to have it signed with HMAC-SHA256.",
    },
    {
        value: "email",
        label: "Email",
        icon: Mail,
        placeholder: "ops@example.com",
        help: "Needs a working platform mail transport on this deployment.",
    },
];

function typeDef(t: NotifyChannelType) {
    return TYPES.find((x) => x.value === t) ?? TYPES[0];
}

function newChannel(): NotifyChannel {
    return {
        // The server assigns a real id on save; this one only has to be unique
        // within the unsaved list so React can key it.
        id: `new-${Math.random().toString(36).slice(2, 10)}`,
        name: "",
        type: "discord",
        target: "",
        secret: "",
        events: [],
        enabled: true,
    };
}

export default function NotificationsPage() {
    const queryClient = useQueryClient();
    const settings = useQuery({ queryKey: SETTINGS_KEY, queryFn: getInstanceSettings });
    const catalog = useQuery({ queryKey: EVENTS_KEY, queryFn: getNotificationEvents });

    const [channels, setChannels] = useState<NotifyChannel[] | null>(null);
    const [testing, setTesting] = useState<string | null>(null);

    useEffect(() => {
        if (settings.data) {
            setChannels(settings.data.notifications?.channels ?? []);
        }
    }, [settings.data]);

    const save = useMutation({
        mutationFn: (next: NotifyChannel[]) => {
            const base = settings.data as InstanceSettings;
            return putInstanceSettings({
                ...base,
                notifications: { channels: next },
            });
        },
        onSuccess: (doc) => {
            queryClient.setQueryData(SETTINGS_KEY, doc);
            setChannels(doc.notifications?.channels ?? []);
            toast.success("Notification channels saved");
        },
        onError: (e: Error) => toast.error(e.message || "Could not save channels"),
    });

    const groups = useMemo(() => {
        const events = catalog.data?.events ?? [];
        const order: string[] = [];
        const byGroup = new Map<string, NotifyEventDef[]>();
        for (const e of events) {
            if (!byGroup.has(e.group)) {
                byGroup.set(e.group, []);
                order.push(e.group);
            }
            byGroup.get(e.group)!.push(e);
        }
        return order.map((g) => ({ group: g, events: byGroup.get(g)! }));
    }, [catalog.data]);

    if (settings.isError) {
        return <ErrorState error={settings.error as Error} onRetry={() => settings.refetch()} />;
    }

    function update(id: string, patch: Partial<NotifyChannel>) {
        setChannels((prev) => (prev ?? []).map((c) => (c.id === id ? { ...c, ...patch } : c)));
    }

    function remove(id: string) {
        setChannels((prev) => (prev ?? []).filter((c) => c.id !== id));
    }

    function toggleEvent(id: string, key: string, on: boolean) {
        setChannels((prev) =>
            (prev ?? []).map((c) => {
                if (c.id !== id) return c;
                const next = on ? [...c.events, key] : c.events.filter((e) => e !== key);
                return { ...c, events: next };
            }),
        );
    }

    async function sendTest(ch: NotifyChannel) {
        setTesting(ch.id);
        try {
            // A saved channel is tested by id so the server uses its stored
            // credential; an unsaved one carries its fields inline.
            const saved = !ch.id.startsWith("new-");
            await testNotificationChannel(
                saved
                    ? { id: ch.id }
                    : { type: ch.type, name: ch.name, target: ch.target, secret: ch.secret },
            );
            toast.success("Test alert delivered");
        } catch (e) {
            toast.error((e as Error).message || "Delivery failed");
        } finally {
            setTesting(null);
        }
    }

    const list = channels ?? [];
    const dirty =
        !!settings.data &&
        JSON.stringify(list) !== JSON.stringify(settings.data.notifications?.channels ?? []);
    // Changing a channel's type clears its target on purpose, so a save with
    // one still empty would drop the channel server-side. Block it here and
    // say which one needs attention.
    const incomplete = list.filter((c) => !c.target.trim());

    return (
        <div className="space-y-6">
            <PageHeader
                title="Notifications"
                description="Where this instance tells you something happened. Add a Discord or Slack webhook, a signed endpoint, or an address."
            >
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setChannels([...(channels ?? []), newChannel()])}
                >
                    <Plus className="h-4 w-4" />
                    Add channel
                </Button>
                <Button
                    size="sm"
                    disabled={!dirty || incomplete.length > 0 || save.isPending}
                    title={
                        incomplete.length > 0
                            ? "Every channel needs a destination before you can save"
                            : undefined
                    }
                    onClick={() => save.mutate(list)}
                >
                    <Save className="h-4 w-4" />
                    {save.isPending ? "Saving…" : "Save"}
                </Button>
            </PageHeader>

            {settings.isLoading ? (
                <Skeleton className="h-64 w-full" />
            ) : list.length === 0 ? (
                <Card>
                    <CardContent className="py-10 text-center">
                        <Bell className="mx-auto mb-3 h-8 w-8 text-muted-foreground" />
                        <p className="text-sm font-medium">No channels yet</p>
                        <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
                            Nothing is being sent anywhere. Add a channel and pick the events it
                            should receive; leave every event unchecked to receive all of them.
                        </p>
                        <Button
                            className="mt-4"
                            size="sm"
                            onClick={() => setChannels([newChannel()])}
                        >
                            <Plus className="h-4 w-4" />
                            Add a channel
                        </Button>
                    </CardContent>
                </Card>
            ) : (
                <div className="space-y-4">
                    {list.map((ch) => {
                        const def = typeDef(ch.type);
                        const Icon = def.icon;
                        const saved = !ch.id.startsWith("new-");
                        return (
                            <Card key={ch.id}>
                                <CardHeader className="pb-3">
                                    <div className="flex flex-wrap items-start justify-between gap-3">
                                        <div className="flex items-start gap-3">
                                            <span className="mt-0.5 flex h-8 w-8 items-center justify-center rounded-md bg-muted">
                                                <Icon className="h-4 w-4" />
                                            </span>
                                            <div>
                                                <CardTitle className="text-base">
                                                    {ch.name || def.label}
                                                </CardTitle>
                                                <CardDescription>
                                                    {ch.events.length === 0
                                                        ? "Receives every event"
                                                        : `Receives ${ch.events.length} event${ch.events.length === 1 ? "" : "s"}`}
                                                    {!saved && " · unsaved"}
                                                </CardDescription>
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            {!ch.enabled && <Badge variant="outline">Off</Badge>}
                                            <Switch
                                                checked={ch.enabled}
                                                onCheckedChange={(v) => update(ch.id, { enabled: v })}
                                            />
                                            <Button
                                                variant="outline"
                                                size="sm"
                                                disabled={testing === ch.id || !ch.target}
                                                onClick={() => sendTest(ch)}
                                            >
                                                <Send className="h-4 w-4" />
                                                {testing === ch.id ? "Sending…" : "Test"}
                                            </Button>
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                onClick={() => remove(ch.id)}
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </Button>
                                        </div>
                                    </div>
                                </CardHeader>
                                <CardContent className="space-y-4">
                                    <div className="grid gap-4 md:grid-cols-3">
                                        <div className="space-y-1.5">
                                            <Label>Type</Label>
                                            <div className="flex flex-wrap gap-1.5">
                                                {TYPES.map((t) => (
                                                    <Button
                                                        key={t.value}
                                                        type="button"
                                                        variant={ch.type === t.value ? "default" : "outline"}
                                                        size="sm"
                                                        onClick={() =>
                                                            update(ch.id, { type: t.value, target: "" })
                                                        }
                                                    >
                                                        {t.label}
                                                    </Button>
                                                ))}
                                            </div>
                                        </div>
                                        <div className="space-y-1.5">
                                            <Label htmlFor={`name-${ch.id}`}>Name</Label>
                                            <Input
                                                id={`name-${ch.id}`}
                                                value={ch.name}
                                                placeholder={def.label}
                                                onChange={(e) => update(ch.id, { name: e.target.value })}
                                            />
                                        </div>
                                        <div className="space-y-1.5">
                                            <Label htmlFor={`target-${ch.id}`}>
                                                {ch.type === "email" ? "Address" : "Webhook URL"}
                                            </Label>
                                            <Input
                                                id={`target-${ch.id}`}
                                                value={ch.target}
                                                placeholder={def.placeholder}
                                                onChange={(e) => update(ch.id, { target: e.target.value })}
                                            />
                                            {ch.target.trim() ? (
                                                <p className="text-xs text-muted-foreground">{def.help}</p>
                                            ) : (
                                                <p className="text-xs text-destructive">
                                                    {ch.type === "email"
                                                        ? "Enter an address before saving."
                                                        : "Enter the webhook URL for this transport before saving."}
                                                </p>
                                            )}
                                        </div>
                                    </div>

                                    {ch.type === "webhook" && (
                                        <div className="space-y-1.5 md:max-w-sm">
                                            <Label htmlFor={`secret-${ch.id}`}>Signing secret</Label>
                                            <Input
                                                id={`secret-${ch.id}`}
                                                value={ch.secret ?? ""}
                                                placeholder="Optional"
                                                onChange={(e) => update(ch.id, { secret: e.target.value })}
                                            />
                                            <p className="text-xs text-muted-foreground">
                                                Signs the body as{" "}
                                                <code>X-Warmbly-Signature: t=&lt;unix&gt;,v1=&lt;hex&gt;</code>, the
                                                same scheme customer webhooks use.
                                            </p>
                                        </div>
                                    )}

                                    <div className="space-y-2">
                                        <div className="flex items-center justify-between">
                                            <Label>Events</Label>
                                            <button
                                                type="button"
                                                className="text-xs text-muted-foreground hover:text-foreground"
                                                onClick={() => update(ch.id, { events: [] })}
                                            >
                                                Receive everything
                                            </button>
                                        </div>
                                        {catalog.isLoading ? (
                                            <Skeleton className="h-24 w-full" />
                                        ) : (
                                            <div className="grid gap-4 md:grid-cols-2">
                                                {groups.map(({ group, events }) => (
                                                    <div key={group} className="space-y-2">
                                                        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                                                            {group}
                                                        </p>
                                                        {events.map((e) => (
                                                            <label
                                                                key={e.key}
                                                                className="flex cursor-pointer items-start gap-2"
                                                            >
                                                                <Checkbox
                                                                    checked={ch.events.includes(e.key)}
                                                                    onCheckedChange={(v) =>
                                                                        toggleEvent(ch.id, e.key, v === true)
                                                                    }
                                                                />
                                                                <span className="text-sm leading-tight">
                                                                    {e.label}
                                                                    <span className="block text-xs text-muted-foreground">
                                                                        {e.description}
                                                                    </span>
                                                                </span>
                                                            </label>
                                                        ))}
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                        {ch.events.length === 0 && (
                                            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                                                <Check className="h-3 w-3" />
                                                Nothing selected, so this channel receives every event.
                                            </p>
                                        )}
                                    </div>
                                </CardContent>
                            </Card>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
