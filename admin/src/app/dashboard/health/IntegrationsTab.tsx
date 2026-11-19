// Integrations tab of Setup and health: the GraphKit connection behind the domain and mailbox store.

import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, Check, Copy, FlaskConical, Save, Unplug } from "lucide-react";
import { useConfirm } from "@/components/ConfirmDialog";
import { ErrorState } from "@/components/ErrorState";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Callout, Panel, StatusBadge } from "@/components/ui/kit";
import { Skeleton } from "@/components/ui/skeleton";
import { INSTANCE_HEALTH_KEY } from "@/hooks/useInstanceHealth";
import { APIError, ReauthCancelledError } from "@/lib/api/client";
import {
    deleteStoreConnection,
    getStoreConnection,
    saveStoreConnection,
    STORE_CONNECTION_KEY,
    testStoreConnection,
    type StoreConnection,
    type StoreConnectionInput,
    type StoreConnectionTest,
} from "@/lib/api/client/admin/store";
import { MODE_TONE } from "../store/format";

export function IntegrationsTab() {
    return (
        <div className="mt-4 space-y-4">
            <StoreConnectionCard />
        </div>
    );
}

const ERROR_TEXT: Record<string, string> = {
    store_configured_by_environment: "GRAPHKIT_* environment variables configure the store, so it cannot be changed here.",
    store_sealing_unavailable: "Set CREDENTIALS_ENCRYPTION_KEY before saving a GraphKit API key here.",
    graphkit_url_invalid: "The API URL is not a valid GraphKit URL.",
    graphkit_key_required: "Enter an API key. None is stored yet.",
    graphkit_key_reentry_required: "Enter the API key again to use a different API URL.",
    graphkit_key_rejected: "GraphKit rejected this API key.",
    graphkit_unreachable: "GraphKit could not be reached at this URL.",
};

function errorText(e: unknown): string {
    if (e instanceof APIError && e.code && ERROR_TEXT[e.code]) return ERROR_TEXT[e.code];
    return e instanceof Error && e.message ? e.message : "The request failed";
}

function trimSlash(u: string): string {
    return u.replace(/\/+$/, "");
}

function money(micros: number, currency: string): string {
    try {
        return new Intl.NumberFormat("en-US", { style: "currency", currency: currency || "USD" }).format(micros / 1_000_000);
    } catch {
        return `${(micros / 1_000_000).toFixed(2)} ${currency}`;
    }
}

const MODE_LABEL: Record<StoreConnection["mode"], string> = {
    live: "Live",
    sandbox: "Sandbox",
    simulator: "Simulator",
    off: "Off",
};

export function StoreConnectionCard() {
    const q = useQuery({ queryKey: STORE_CONNECTION_KEY, queryFn: getStoreConnection, retry: false });
    return (
        <Panel
            title="Domains and mailboxes (GraphKit)"
            description="Buying domains and mailboxes needs only a GraphKit API key. The webhook secret and API URL are optional."
            actions={q.data && <StatusBadge tone={MODE_TONE[q.data.mode] ?? "neutral"} dot>{MODE_LABEL[q.data.mode] ?? q.data.mode}</StatusBadge>}
        >
            {q.isLoading ? (
                <div className="space-y-3">
                    <Skeleton className="h-8 w-full" />
                    <Skeleton className="h-8 w-full" />
                    <Skeleton className="h-8 w-2/3" />
                </div>
            ) : q.isError || !q.data ? (
                <ErrorState error={q.error} title="Couldn’t load the GraphKit connection" onRetry={() => void q.refetch()} />
            ) : (
                // Remount on every server change so the form starts from what is stored.
                <ConnectionForm key={`${q.data.source}:${q.data.updated_at ?? ""}`} conn={q.data} />
            )}
        </Panel>
    );
}

function SecretStatus({ set }: { set: boolean }) {
    return <StatusBadge tone={set ? "success" : "neutral"}>{set ? "Set" : "Not set"}</StatusBadge>;
}

function ConnectionForm({ conn }: { conn: StoreConnection }) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const fromEnv = conn.source === "environment";
    const [apiUrl, setApiUrl] = useState(conn.api_url);
    const [apiKey, setApiKey] = useState("");
    const [secret, setSecret] = useState("");
    const [result, setResult] = useState<{ ok: StoreConnectionTest } | { error: string } | null>(null);

    const input = (): StoreConnectionInput => ({
        api_url: apiUrl.trim() || conn.default_api_url,
        api_key: apiKey.trim() || undefined,
        webhook_secret: secret.trim() || undefined,
    });

    async function refresh(next: StoreConnection) {
        qc.setQueryData(STORE_CONNECTION_KEY, next);
        await Promise.all([
            qc.invalidateQueries({ queryKey: ["admin", "store"] }),
            qc.invalidateQueries({ queryKey: INSTANCE_HEALTH_KEY }),
        ]);
    }

    const testMut = useMutation({
        mutationFn: testStoreConnection,
        onSuccess: (ok) => setResult({ ok }),
        onError: (e) => {
            if (!(e instanceof ReauthCancelledError)) setResult({ error: errorText(e) });
        },
    });
    const saveMut = useMutation({
        mutationFn: saveStoreConnection,
        onSuccess: async (next) => {
            toast.success("GraphKit API key saved");
            await refresh(next);
        },
        onError: (e) => {
            if (!(e instanceof ReauthCancelledError)) toast.error(errorText(e));
        },
    });
    const deleteMut = useMutation({
        mutationFn: deleteStoreConnection,
        onSuccess: async (next) => {
            toast.success("GraphKit disconnected");
            await refresh(next);
        },
        onError: (e) => {
            if (!(e instanceof ReauthCancelledError)) toast.error(errorText(e));
        },
    });

    const busy = testMut.isPending || saveMut.isPending || deleteMut.isPending;
    // The stored key is only sent to the URL it was saved with.
    const urlChanged = conn.api_key_set && trimSlash(apiUrl.trim() || conn.default_api_url) !== trimSlash(conn.api_url);
    const keyMissing = (!conn.api_key_set || urlChanged) && !apiKey.trim();
    const saveBlocked = !conn.sealing_available
        ? "Set CREDENTIALS_ENCRYPTION_KEY to save a GraphKit API key here."
        : keyMissing
            ? urlChanged
                ? "Enter the API key again to use a different API URL."
                : "Enter an API key to save. None is stored yet."
            : null;

    function edit(set: (v: string) => void) {
        return (v: string) => {
            set(v);
            setResult(null);
        };
    }

    function onSubmit(e: FormEvent) {
        e.preventDefault();
        if (fromEnv || saveBlocked || busy) return;
        saveMut.mutate(input());
    }

    async function disconnect() {
        const ok = await confirm({
            title: "Disconnect GraphKit?",
            description:
                "The store stops now. New purchases, renewals, monthly billing and Google mailbox connections pause until an API key is added again. The stored API key and webhook secret are deleted.",
            confirmLabel: "Disconnect",
            destructive: true,
        });
        if (ok) deleteMut.mutate();
    }

    const pending = conn.purchases_without_connection;

    return (
        <form className="space-y-4" onSubmit={onSubmit}>
            {pending && (
                <Callout tone="danger" icon={AlertTriangle} title="Store purchases have no GraphKit API key">
                    {pending.mailboxes} {pending.mailboxes === 1 ? "mailbox" : "mailboxes"} and {pending.domains}{" "}
                    {pending.domains === 1 ? "domain" : "domains"} were bought here. Renewals, monthly billing and Google
                    mailbox connections are paused until GraphKit is configured.
                </Callout>
            )}

            {fromEnv ? (
                <div className="flex flex-wrap items-center gap-2 text-[12.5px] text-muted-foreground">
                    <StatusBadge tone="info">Configured by the environment</StatusBadge>
                    <span>GRAPHKIT_* environment variables win over these settings.</span>
                </div>
            ) : (
                !conn.sealing_available && (
                    <Callout tone="warning" icon={AlertTriangle} title="Encryption key required">
                        CREDENTIALS_ENCRYPTION_KEY must be set before GraphKit can be saved here, because the API key and
                        webhook secret are stored encrypted with it.
                    </Callout>
                )
            )}

            <div className="grid gap-4 md:grid-cols-3">
                <div className="space-y-1.5">
                    <Label htmlFor="graphkit-url">API URL</Label>
                    {fromEnv ? (
                        <div className="break-all font-mono text-[12.5px] text-foreground">{conn.api_url}</div>
                    ) : (
                        <Input
                            id="graphkit-url"
                            value={apiUrl}
                            placeholder={conn.default_api_url}
                            onChange={(e) => edit(setApiUrl)(e.target.value)}
                            autoComplete="off"
                            spellCheck={false}
                        />
                    )}
                </div>
                <div className="space-y-1.5">
                    <Label htmlFor="graphkit-key">
                        API key <SecretStatus set={conn.api_key_set} />
                    </Label>
                    {!fromEnv && (
                        <Input
                            id="graphkit-key"
                            type="password"
                            value={apiKey}
                            onChange={(e) => edit(setApiKey)(e.target.value)}
                            autoComplete="new-password"
                            placeholder={conn.api_key_set && !urlChanged ? "Leave blank to keep the stored key" : "Required"}
                        />
                    )}
                </div>
                <div className="space-y-1.5">
                    <Label htmlFor="graphkit-secret">
                        Webhook secret <SecretStatus set={conn.webhook_secret_set} />
                    </Label>
                    {!fromEnv && (
                        <Input
                            id="graphkit-secret"
                            type="password"
                            value={secret}
                            onChange={(e) => edit(setSecret)(e.target.value)}
                            autoComplete="new-password"
                            placeholder={conn.webhook_secret_set ? "Leave blank to keep the stored secret" : "From GraphKit"}
                        />
                    )}
                </div>
            </div>
            {!fromEnv && (
                <p className="text-xs text-muted-foreground">
                    Secrets are never shown again once saved. Leave a field blank to keep the stored value.
                </p>
            )}

            <div className="space-y-1.5">
                <Label htmlFor="graphkit-webhook">Warmbly webhook URL</Label>
                <div className="flex items-center gap-2">
                    <Input id="graphkit-webhook" value={conn.webhook_url} readOnly className="font-mono text-[12.5px]" onFocus={(e) => e.target.select()} />
                    <CopyButton value={conn.webhook_url} />
                </div>
                <p className="text-xs text-muted-foreground">
                    In GraphKit, register this URL as your webhook endpoint and paste the signing secret it gives you above.
                </p>
            </div>

            {result && <TestResult result={result} />}

            {!fromEnv && (
                <div className="flex flex-wrap items-center gap-2 border-t border-border pt-4">
                    <Button type="submit" size="sm" disabled={!!saveBlocked || busy}>
                        <Save />
                        {saveMut.isPending ? "Saving…" : "Save"}
                    </Button>
                    <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => testMut.mutate(input())}>
                        <FlaskConical />
                        {testMut.isPending ? "Testing…" : "Test connection"}
                    </Button>
                    {conn.source === "settings" && (
                        <Button type="button" size="sm" variant="ghost" className="text-destructive hover:text-destructive" disabled={busy} onClick={() => void disconnect()}>
                            <Unplug />
                            Disconnect
                        </Button>
                    )}
                    {saveBlocked && <span className="text-xs text-muted-foreground">{saveBlocked}</span>}
                    {!saveBlocked && conn.updated_at && (
                        <span className="ml-auto text-xs text-muted-foreground">Saved {new Date(conn.updated_at).toLocaleString()}</span>
                    )}
                </div>
            )}
        </form>
    );
}

function TestResult({ result }: { result: { ok: StoreConnectionTest } | { error: string } }) {
    if ("error" in result) {
        return <Callout tone="danger" icon={AlertTriangle} title="Connection test failed">{result.error}</Callout>;
    }
    const t = result.ok;
    const com =
        t.com_register_micros != null
            ? `.com ${money(t.com_register_micros, t.currency)} to register${t.com_renew_micros != null ? `, ${money(t.com_renew_micros, t.currency)} to renew` : ""}`
            : null;
    return (
        <Callout tone="success" icon={Check} title={t.mode === "live" ? "Connected to a live GraphKit account" : "Connected to a GraphKit test account"}>
            Balance {money(t.balance_micros, t.currency)}
            {com && `. ${com}`}
        </Callout>
    );
}

function CopyButton({ value }: { value: string }) {
    const [copied, setCopied] = useState(false);
    return (
        <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={async () => {
                try {
                    await navigator.clipboard.writeText(value);
                    setCopied(true);
                    toast.success("Webhook URL copied");
                    window.setTimeout(() => setCopied(false), 1500);
                } catch {
                    toast.error("Could not copy. Select the URL and copy it instead.");
                }
            }}
        >
            {copied ? <Check /> : <Copy />}
            {copied ? "Copied" : "Copy"}
        </Button>
    );
}
