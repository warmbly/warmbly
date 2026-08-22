// Instance settings: the only writable configuration in the product. These
// keys are deliberately disjoint from the environment, so there is no
// precedence to resolve and nothing here can be overwritten at the next boot.

import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Save, SlidersHorizontal } from "lucide-react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
    getInstanceSettings,
    putInstanceSettings,
    type InstanceSettings,
} from "@/lib/api/client/admin/instance";

const SETTINGS_KEY = ["admin", "instance", "settings"];

// The backend clamps to the same band; matching it here keeps the operator
// from spending a round-trip to learn the range.
const TTL_MIN_HOURS = 1;
const TTL_MAX_HOURS = 720;

// Sending-domain authentication grace, mirroring internal/app/instancesettings.
const AUTH_GRACE_MIN_HOURS = 1;
const AUTH_GRACE_MAX_HOURS = 720;

// Mailbox sync fair-use bands, mirroring internal/config/constants.go.
const SYNC_FIELDS = [
    {
        key: "backfillDays",
        setting: "backfill_days",
        label: "Import window (days)",
        min: 1,
        max: 730,
        help: "How far back the initial import reaches when a mailbox is connected. Newest mail first.",
    },
    {
        key: "backfillMessages",
        setting: "backfill_messages",
        label: "Import cap (messages per mailbox)",
        min: 1,
        max: 100000,
        help: "The most messages the initial import stores for one mailbox, whatever the window holds.",
    },
    {
        key: "dailyPerMailbox",
        setting: "daily_messages_per_mailbox",
        label: "Daily budget (messages per mailbox)",
        min: 1,
        max: 100000,
        help: "New mail one mailbox may store per UTC day. Over it, mail waits for the next day; replies to the mailbox's own sends have a separate budget of the same size and keep landing.",
    },
    {
        key: "dailyPerOrg",
        setting: "daily_messages_per_org",
        label: "Daily budget (messages per organization)",
        min: 1,
        max: 2000000,
        help: "New plus imported mail across one organization per UTC day.",
    },
] as const;

type SyncFieldKey = (typeof SYNC_FIELDS)[number]["key"];

interface FormState {
    linksEnabled: boolean;
    ttlHours: string;
    allowInvitedSignup: boolean;
    sync: Record<SyncFieldKey, string>;
    enforceDomainAuth: boolean;
    authGraceHours: string;
}

function toForm(s: InstanceSettings): FormState {
    return {
        linksEnabled: s.invitations.links_enabled,
        ttlHours: String(s.invitations.ttl_hours),
        allowInvitedSignup: s.access.allow_invited_signup,
        sync: {
            backfillDays: String(s.sync.backfill_days),
            backfillMessages: String(s.sync.backfill_messages),
            dailyPerMailbox: String(s.sync.daily_messages_per_mailbox),
            dailyPerOrg: String(s.sync.daily_messages_per_org),
        },
        enforceDomainAuth: s.deliverability.enforce_domain_auth,
        authGraceHours: String(s.deliverability.auth_grace_hours),
    };
}

function syncFieldValid(raw: string, min: number, max: number): boolean {
    const n = Number(raw);
    return raw.trim() !== "" && Number.isInteger(n) && n >= min && n <= max;
}

export default function InstanceSettingsPage() {
    const qc = useQueryClient();
    const [form, setForm] = useState<FormState | null>(null);

    const settingsQ = useQuery({
        queryKey: SETTINGS_KEY,
        queryFn: getInstanceSettings,
        retry: false,
    });

    // Reseed whenever a fresh document arrives so a background refetch does
    // not silently keep an operator editing a stale form.
    useEffect(() => {
        if (settingsQ.data) setForm(toForm(settingsQ.data));
    }, [settingsQ.data]);

    const saveMut = useMutation({
        mutationFn: (body: InstanceSettings) => putInstanceSettings(body),
        onSuccess: (saved) => {
            qc.setQueryData(SETTINGS_KEY, saved);
            setForm(toForm(saved));
            toast.success("Instance settings saved");
        },
        onError: (err: Error) => toast.error(err.message || "Could not save settings"),
    });

    const server = settingsQ.data;
    const syncDirty =
        !!server &&
        !!form &&
        SYNC_FIELDS.some((f) => form.sync[f.key] !== String(server.sync[f.setting]));
    const dirty =
        !!server &&
        !!form &&
        (form.linksEnabled !== server.invitations.links_enabled ||
            form.ttlHours !== String(server.invitations.ttl_hours) ||
            form.allowInvitedSignup !== server.access.allow_invited_signup ||
            form.enforceDomainAuth !== server.deliverability.enforce_domain_auth ||
            form.authGraceHours !== String(server.deliverability.auth_grace_hours) ||
            syncDirty);
    const syncValid =
        form !== null && SYNC_FIELDS.every((f) => syncFieldValid(form.sync[f.key], f.min, f.max));

    const authGrace = form ? Number(form.authGraceHours) : NaN;
    const authGraceValid =
        form !== null &&
        form.authGraceHours.trim() !== "" &&
        Number.isInteger(authGrace) &&
        authGrace >= AUTH_GRACE_MIN_HOURS &&
        authGrace <= AUTH_GRACE_MAX_HOURS;

    const ttl = form ? Number(form.ttlHours) : NaN;
    const ttlValid =
        form !== null &&
        form.ttlHours.trim() !== "" &&
        Number.isInteger(ttl) &&
        ttl >= TTL_MIN_HOURS &&
        ttl <= TTL_MAX_HOURS;

    function save() {
        if (!form) return;
        if (!ttlValid) {
            toast.error(
                `Invitation validity must be a whole number of hours between ${TTL_MIN_HOURS} and ${TTL_MAX_HOURS}`,
            );
            return;
        }
        if (!syncValid) {
            toast.error("Every sync budget must be a whole number inside its range");
            return;
        }
        if (!authGraceValid) {
            toast.error(
                `The authentication grace period must be a whole number of hours between ${AUTH_GRACE_MIN_HOURS} and ${AUTH_GRACE_MAX_HOURS}`,
            );
            return;
        }
        saveMut.mutate({
            invitations: { links_enabled: form.linksEnabled, ttl_hours: ttl },
            access: { allow_invited_signup: form.allowInvitedSignup },
            sync: {
                backfill_days: Number(form.sync.backfillDays),
                backfill_messages: Number(form.sync.backfillMessages),
                daily_messages_per_mailbox: Number(form.sync.dailyPerMailbox),
                daily_messages_per_org: Number(form.sync.dailyPerOrg),
            },
            deliverability: {
                enforce_domain_auth: form.enforceDomainAuth,
                auth_grace_hours: authGrace,
            },
        });
    }

    return (
        <div>
            <PageHeader
                title="Instance settings"
                description="These settings are stored in the database and are not read from the environment."
            >
                <Button size="sm" variant="outline" asChild>
                    <Link to="/configuration">
                        <SlidersHorizontal className="size-4" />
                        Configuration
                    </Link>
                </Button>
                <Button
                    size="sm"
                    onClick={save}
                    disabled={!dirty || saveMut.isPending || !form}
                >
                    <Save className="size-4" />
                    {saveMut.isPending ? "Saving..." : "Save changes"}
                </Button>
            </PageHeader>

            {settingsQ.isLoading && (
                <div className="space-y-3">
                    <Skeleton className="h-40 w-full" />
                    <Skeleton className="h-28 w-full" />
                </div>
            )}

            {settingsQ.isError && (
                <ErrorState
                    error={settingsQ.error}
                    title="Could not load instance settings"
                    onRetry={() => settingsQ.refetch()}
                />
            )}

            {form && (
                <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
                    <Card>
                        <CardHeader>
                            <CardTitle>Invitations</CardTitle>
                            <CardDescription>
                                How people are brought into a workspace from Settings, Members in
                                the dashboard.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="space-y-3 pt-0">
                            <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
                                <div className="min-w-0">
                                    <p className="text-sm font-medium">Invitation links</p>
                                    <p className="text-xs text-muted-foreground">
                                        Show a copyable link next to each pending invitation. Leave
                                        this on when the platform mail transport does not deliver,
                                        otherwise an invited person never receives anything.
                                    </p>
                                </div>
                                <Switch
                                    checked={form.linksEnabled}
                                    onCheckedChange={(v) =>
                                        setForm({ ...form, linksEnabled: v })
                                    }
                                />
                            </div>

                            <div>
                                <Label htmlFor="ttl-hours">Invitation validity (hours)</Label>
                                {/* Text, not number: the native spinner is not ours, and the value is already validated as a string. */}
                                <Input
                                    id="ttl-hours"
                                    type="text"
                                    inputMode="numeric"
                                    autoComplete="off"
                                    value={form.ttlHours}
                                    onChange={(e) =>
                                        setForm({ ...form, ttlHours: e.target.value })
                                    }
                                    aria-invalid={!ttlValid}
                                    className="mt-1"
                                />
                                <p className="mt-1 text-xs text-muted-foreground">
                                    Between {TTL_MIN_HOURS} and {TTL_MAX_HOURS} hours (30 days).
                                    Existing invitations keep the expiry they were issued with.
                                </p>
                                {!ttlValid && (
                                    <p className="mt-1 text-xs text-red-600">
                                        Enter a whole number of hours between {TTL_MIN_HOURS} and{" "}
                                        {TTL_MAX_HOURS}.
                                    </p>
                                )}
                            </div>
                        </CardContent>
                    </Card>

                    <Card>
                        <CardHeader>
                            <CardTitle>Access</CardTitle>
                            <CardDescription>
                                Who may create an account on this instance. The registration mode
                                itself is owned by the environment and is listed on Configuration.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="pt-0">
                            <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
                                <div className="min-w-0">
                                    <p className="text-sm font-medium">Allow invited sign-up</p>
                                    <p className="text-xs text-muted-foreground">
                                        Someone holding a valid invitation can create an account
                                        even though open sign-ups are closed. Turning this off
                                        means only existing accounts can sign in.
                                    </p>
                                </div>
                                <Switch
                                    checked={form.allowInvitedSignup}
                                    onCheckedChange={(v) =>
                                        setForm({ ...form, allowInvitedSignup: v })
                                    }
                                />
                            </div>
                        </CardContent>
                    </Card>

                    <Card className="lg:col-span-2">
                        <CardHeader>
                            <CardTitle>Mailbox sync fair use</CardTitle>
                            <CardDescription>
                                What a connected mailbox imports and how much new mail it may
                                store. Mail over a budget waits and is picked up when the window
                                rolls; nothing is dropped, and replies to the mailbox&apos;s own
                                outreach are never held. Changes apply the next time a mailbox is
                                loaded onto a worker (within a few minutes). The fixed pacing
                                numbers are listed under Limits.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="grid grid-cols-1 gap-3 pt-0 md:grid-cols-2">
                            {SYNC_FIELDS.map((f) => {
                                const valid = syncFieldValid(form.sync[f.key], f.min, f.max);
                                return (
                                    <div key={f.key}>
                                        <Label htmlFor={`sync-${f.key}`}>{f.label}</Label>
                                        <Input
                                            id={`sync-${f.key}`}
                                            type="text"
                                            inputMode="numeric"
                                            autoComplete="off"
                                            value={form.sync[f.key]}
                                            onChange={(e) =>
                                                setForm({
                                                    ...form,
                                                    sync: { ...form.sync, [f.key]: e.target.value },
                                                })
                                            }
                                            aria-invalid={!valid}
                                            className="mt-1"
                                        />
                                        <p className="mt-1 text-xs text-muted-foreground">
                                            {f.help} Between {f.min.toLocaleString()} and{" "}
                                            {f.max.toLocaleString()}.
                                        </p>
                                        {!valid && (
                                            <p className="mt-1 text-xs text-red-600">
                                                Enter a whole number between {f.min.toLocaleString()}{" "}
                                                and {f.max.toLocaleString()}.
                                            </p>
                                        )}
                                    </div>
                                );
                            })}
                        </CardContent>
                    </Card>

                    <Card className="lg:col-span-2">
                        <CardHeader>
                            <CardTitle>Sending-domain authentication</CardTitle>
                            <CardDescription>
                                Gmail, Yahoo, and Outlook reject or spam-filter mail from a domain
                                without SPF and DMARC, and one unauthenticated sender damages the
                                reputation of every mailbox in the shared warmup pool. Warmbly
                                checks each sending domain daily and can stop cold sending and
                                warmup from a domain that keeps failing. The grace period is how
                                long a domain may keep failing first, so a DNS outage cannot stop
                                a customer&apos;s campaigns and the owner is warned throughout it.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="space-y-4 pt-0">
                            <div className="flex items-start justify-between gap-4">
                                <div className="space-y-0.5">
                                    <Label>Stop sending from unauthenticated domains</Label>
                                    <p className="text-xs text-muted-foreground">
                                        Off keeps the check informational: domains are still
                                        checked, shown on the mailbox, and raised by the advisor,
                                        but nothing is ever blocked.
                                    </p>
                                </div>
                                <Switch
                                    checked={form.enforceDomainAuth}
                                    onCheckedChange={(v) =>
                                        setForm({ ...form, enforceDomainAuth: v })
                                    }
                                />
                            </div>
                            <div className="md:max-w-sm">
                                <Label htmlFor="auth-grace-hours">Grace period (hours)</Label>
                                <Input
                                    id="auth-grace-hours"
                                    type="text"
                                    inputMode="numeric"
                                    autoComplete="off"
                                    value={form.authGraceHours}
                                    onChange={(e) =>
                                        setForm({ ...form, authGraceHours: e.target.value })
                                    }
                                    aria-invalid={!authGraceValid}
                                    disabled={!form.enforceDomainAuth}
                                    className="mt-1"
                                />
                                <p className="mt-1 text-xs text-muted-foreground">
                                    How long a domain must stay failing before its mailboxes stop
                                    sending. Between {AUTH_GRACE_MIN_HOURS} and{" "}
                                    {AUTH_GRACE_MAX_HOURS.toLocaleString()}.
                                </p>
                                {!authGraceValid && (
                                    <p className="mt-1 text-xs text-red-600">
                                        Enter a whole number between {AUTH_GRACE_MIN_HOURS} and{" "}
                                        {AUTH_GRACE_MAX_HOURS.toLocaleString()}.
                                    </p>
                                )}
                            </div>
                        </CardContent>
                    </Card>
                </div>
            )}

            {form && dirty && (
                <div className="mt-4 flex items-center gap-2">
                    <Button size="sm" onClick={save} disabled={saveMut.isPending}>
                        <Save className="size-4" />
                        {saveMut.isPending ? "Saving..." : "Save changes"}
                    </Button>
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => server && setForm(toForm(server))}
                        disabled={saveMut.isPending}
                    >
                        Discard
                    </Button>
                    <span className="text-xs text-muted-foreground">
                        Saving records the change in the admin audit log.
                    </span>
                </div>
            )}
        </div>
    );
}
