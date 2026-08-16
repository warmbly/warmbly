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

interface FormState {
    linksEnabled: boolean;
    ttlHours: string;
    allowInvitedSignup: boolean;
}

function toForm(s: InstanceSettings): FormState {
    return {
        linksEnabled: s.invitations.links_enabled,
        ttlHours: String(s.invitations.ttl_hours),
        allowInvitedSignup: s.access.allow_invited_signup,
    };
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
    const dirty =
        !!server &&
        !!form &&
        (form.linksEnabled !== server.invitations.links_enabled ||
            form.ttlHours !== String(server.invitations.ttl_hours) ||
            form.allowInvitedSignup !== server.access.allow_invited_signup);

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
        saveMut.mutate({
            invitations: { links_enabled: form.linksEnabled, ttl_hours: ttl },
            access: { allow_invited_signup: form.allowInvitedSignup },
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
