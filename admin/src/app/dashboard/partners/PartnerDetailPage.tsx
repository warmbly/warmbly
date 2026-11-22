// One partner: its settings, keys, webhook and callback secrets, and the workspaces it manages.
// Reading needs view_organizations; every change needs manage_organizations and a fresh confirmation.

import { useState, type FormEvent } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { Copy, KeyRound, Plus } from "lucide-react";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/PageHeader";
import { Panel, Property, PropertyList, StatusBadge } from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import { DataTable, type Column } from "@/components/data/DataTable";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useConfirm } from "@/components/ConfirmDialog";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { PartnerStatements } from "./PartnerStatements";
import {
    createPartnerKey,
    getPartner,
    listPartnerOrganizations,
    revokePartnerKey,
    rotatePartnerWebhookSecret,
    setPartnerCallbackKey,
    updatePartner,
    type AdminPartnerDetail,
    type AdminPartnerKey,
    type AdminPartnerOrganization,
    type AdminPartnerUpdate,
    type PartnerMode,
} from "@/lib/api/client/admin/partners";

const FIELD_LABEL = "text-xs font-medium text-muted-foreground";
const MICROS = 1_000_000;

function usdFromMicros(m: number): string {
    return (m / MICROS).toLocaleString(undefined, { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 6 });
}

const STATE_TONE = { active: "success", suspended: "warning", cancelled: "danger" } as const;

const ORG_COLUMNS: Column<AdminPartnerOrganization>[] = [
    {
        id: "name",
        header: "Workspace",
        cell: (o) =>
            o.organization_id ? (
                <Link to={`/organizations/${o.organization_id}`} className="font-medium underline-offset-2 hover:underline">
                    {o.organization_name || o.organization_id}
                </Link>
            ) : (
                <span className="text-muted-foreground">Deleted</span>
            ),
        csv: (o) => o.organization_name ?? "",
    },
    { id: "external", header: "Partner's id", cell: (o) => <span className="font-mono text-xs">{o.external_id}</span>, csv: (o) => o.external_id },
    { id: "mode", header: "Mode", cell: (o) => <StatusBadge tone={o.mode === "live" ? "info" : "neutral"}>{o.mode}</StatusBadge>, csv: (o) => o.mode },
    {
        id: "state",
        header: "State",
        cell: (o) => (
            <StatusBadge tone={STATE_TONE[o.state]} title={o.suspended_reason ?? undefined}>
                {o.state}
            </StatusBadge>
        ),
        csv: (o) => o.state,
    },
    { id: "group", header: "Group", cell: (o) => o.group_name || o.group_external_id || "None", csv: (o) => o.group_external_id ?? "" },
    { id: "cap", header: "Sends / mailbox / day", align: "right", cell: (o) => o.campaign_sends_per_mailbox_per_day, csv: (o) => o.campaign_sends_per_mailbox_per_day },
    {
        id: "deletes",
        header: "Deletes",
        cell: (o) => (o.deletes_at ? new Date(o.deletes_at).toLocaleDateString() : ""),
        csv: (o) => o.deletes_at ?? "",
    },
];

export default function PartnerDetailPage() {
    const { id = "" } = useParams();
    const q = useQuery({ queryKey: ["admin", "partners", id], queryFn: () => getPartner(id), enabled: !!id });
    const p = q.data;

    if (q.error) {
        return (
            <>
                <PageHeader title="Partner" breadcrumbs={[{ label: "Partners", to: "/partners" }]} />
                <ErrorState error={q.error} onRetry={() => void q.refetch()} />
            </>
        );
    }

    return (
        <>
            <PageHeader
                title={p?.name ?? "Partner"}
                breadcrumbs={[{ label: "Partners", to: "/partners" }]}
                meta={p ? <StatusBadge tone={p.status === "active" ? "success" : "warning"}>{p.status}</StatusBadge> : undefined}
            />
            {p && (
                <div className="space-y-4">
                    <PartnerStatusCard partner={p} />
                    <PartnerSettingsCard partner={p} />
                    <PartnerKeysCard partner={p} />
                    <PartnerSecretsCard partner={p} />
                    <PartnerStatements partnerId={p.id} />
                    <PartnerOrganizations partnerId={p.id} />
                </div>
            )}
        </>
    );
}

function usePartnerWrite<T>(id: string, fn: (v: T) => Promise<unknown>, done: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: fn,
        onSuccess: (res) => {
            if (res && typeof res === "object" && "keys" in res) qc.setQueryData(["admin", "partners", id], res as AdminPartnerDetail);
            void qc.invalidateQueries({ queryKey: ["admin", "partners"] });
            toast.success(done);
        },
        onError: (e: Error) => toast.error(e.message || "That change did not save."),
    });
}

function PartnerStatusCard({ partner: p }: { partner: AdminPartnerDetail }) {
    const confirm = useConfirm();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const suspended = p.status === "suspended";
    const toggle = usePartnerWrite(p.id, () => updatePartner(p.id, { status: suspended ? "active" : "suspended" }), suspended ? "Partner resumed" : "Partner suspended");

    async function run() {
        const ok = await confirm({
            title: suspended ? `Resume ${p.name}?` : `Suspend ${p.name}?`,
            description: suspended
                ? "Its keys work again. Workspaces it manages keep their own state."
                : "Every one of its keys answers partner_suspended until it is resumed. Workspaces it manages keep running as they are.",
            confirmLabel: suspended ? "Resume" : "Suspend",
            destructive: !suspended,
        });
        if (ok) await toggle.mutateAsync(undefined).catch(() => undefined);
    }

    return (
        <PropertyList title="Partner">
            <Property label="Slug">
                <span className="font-mono text-xs">{p.slug}</span>
            </Property>
            <Property label="Members see">Billed through {p.display_name}</Property>
            <Property label="Plans">{p.allowed_plans.join(", ") || "None"}</Property>
            <Property label="Status">
                <span className="inline-flex items-center gap-2">
                    {p.status}
                    {canManage && (
                        <Button size="sm" variant="outline" disabled={toggle.isPending} onClick={() => void run()}>
                            {suspended ? "Resume" : "Suspend"}
                        </Button>
                    )}
                </span>
            </Property>
        </PropertyList>
    );
}

function PartnerSettingsCard({ partner: p }: { partner: AdminPartnerDetail }) {
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [form, setForm] = useState(() => ({
        name: p.name,
        display_name: p.display_name,
        allowed_hosts: p.allowed_hosts.join(", "),
        delegation_domains: (p.delegation_domains ?? []).join(", "),
        webhook_url: p.webhook_url ?? "",
        callback_base_url: p.callback_base_url ?? "",
        sending: String(p.wholesale_sending_warmup_day_micros / MICROS),
        warmup: String(p.wholesale_warmup_only_day_micros / MICROS),
        rate: String(p.rate_limit_per_second),
    }));
    const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));
    const save = usePartnerWrite(p.id, (body: AdminPartnerUpdate) => updatePartner(p.id, body), "Partner saved");

    function submit(e: FormEvent) {
        e.preventDefault();
        const toMicros = (v: string) => Math.round(Number(v || "0") * MICROS);
        save.mutate({
            name: form.name.trim(),
            display_name: form.display_name.trim(),
            allowed_hosts: form.allowed_hosts.split(/[\s,]+/).filter(Boolean),
            delegation_domains: form.delegation_domains.split(/[\s,]+/).filter(Boolean),
            webhook_url: form.webhook_url.trim(),
            callback_base_url: form.callback_base_url.trim(),
            wholesale_sending_warmup_day_micros: toMicros(form.sending),
            wholesale_warmup_only_day_micros: toMicros(form.warmup),
            rate_limit_per_second: Number(form.rate),
        });
    }

    const field = (id: keyof typeof form, label: string, hint?: string, placeholder?: string) => (
        <div className="space-y-1.5">
            <Label htmlFor={`partner-${id}`} className={FIELD_LABEL}>
                {label}
            </Label>
            <Input id={`partner-${id}`} value={form[id]} onChange={set(id)} placeholder={placeholder} disabled={!canManage} />
            {hint && <p className="text-xs leading-relaxed text-muted-foreground">{hint}</p>}
        </div>
    );

    return (
        <Panel title="Settings" description="What the partner's workspaces show, where its events go, and what it pays.">
            <form onSubmit={submit} noValidate className="grid gap-4 sm:grid-cols-2">
                {field("name", "Name")}
                {field("display_name", "Display name", 'Members read "Billed through" this name.')}
                {field("allowed_hosts", "Allowed hosts", "Hosts a return URL the partner sends may point at, separated by commas.", "app.example.com")}
                {field("rate", "Requests per second", "20 to 1000, across all of the partner's keys.")}
                {field(
                    "delegation_domains",
                    "Delegation domains",
                    "Domains you have confirmed the partner controls, separated by commas. A mailbox the partner attaches by delegation connects only on one of these, or on a domain its workspace verified.",
                    "mail.example.com",
                )}
                {field("webhook_url", "Webhook URL", "HTTPS. Events are signed with the webhook secret below.", "https://api.example.com/webhooks/warmbly")}
                {field("callback_base_url", "Token callback base", "Where per-mailbox access tokens are requested.", "https://api.example.com/v1/warmbly")}
                {field("sending", "Wholesale, sending and warmup (USD per mailbox-day)", `Now ${usdFromMicros(p.wholesale_sending_warmup_day_micros)}.`)}
                {field("warmup", "Wholesale, warmup only (USD per mailbox-day)", `Now ${usdFromMicros(p.wholesale_warmup_only_day_micros)}.`)}
                {canManage && (
                    <div className="sm:col-span-2 flex justify-end">
                        <Button type="submit" size="sm" disabled={save.isPending}>
                            {save.isPending ? "Saving…" : "Save"}
                        </Button>
                    </div>
                )}
            </form>
        </Panel>
    );
}

function SecretOnce({ secret, onClose, title, hint }: { secret: string | null; onClose: () => void; title: string; hint: string }) {
    return (
        <Dialog open={!!secret} onOpenChange={(v) => !v && onClose()}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>{title}</DialogTitle>
                    <DialogDescription>{hint}</DialogDescription>
                </DialogHeader>
                <div className="flex items-center gap-2">
                    <Input readOnly value={secret ?? ""} className="font-mono text-xs" onFocus={(e) => e.target.select()} />
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                            void navigator.clipboard.writeText(secret ?? "");
                            toast.success("Copied");
                        }}
                    >
                        <Copy className="size-3.5" />
                    </Button>
                </div>
                <DialogFooter>
                    <Button onClick={onClose}>Done</Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function PartnerKeysCard({ partner: p }: { partner: AdminPartnerDetail }) {
    const confirm = useConfirm();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [secret, setSecret] = useState<string | null>(null);
    const create = usePartnerWrite(p.id, async (mode: PartnerMode) => {
        const k = await createPartnerKey(p.id, { mode });
        setSecret(k.secret);
        return k;
    }, "Key created");
    const revoke = usePartnerWrite(p.id, (keyId: string) => revokePartnerKey(p.id, keyId), "Key revoked");

    async function askRevoke(k: AdminPartnerKey) {
        const ok = await confirm({
            title: `Revoke ${k.key_prefix}…${k.key_suffix}?`,
            description: "Anything using this key stops working at once.",
            confirmLabel: "Revoke",
            destructive: true,
        });
        if (ok) await revoke.mutateAsync(k.id).catch(() => undefined);
    }

    return (
        <Panel
            title="API keys"
            description="A live key reaches live workspaces only, a test key test workspaces only."
            actions={
                canManage && (
                    <>
                        <Button size="sm" variant="outline" disabled={create.isPending} onClick={() => create.mutate("test")}>
                            <Plus className="size-3.5" /> Test key
                        </Button>
                        <Button size="sm" disabled={create.isPending} onClick={() => create.mutate("live")}>
                            <Plus className="size-3.5" /> Live key
                        </Button>
                    </>
                )
            }
            bodyClassName="p-0"
        >
            {p.keys.length === 0 ? (
                <p className="px-4 py-3 text-[12.5px] text-muted-foreground">No keys yet.</p>
            ) : (
                <ul className="divide-y divide-border">
                    {p.keys.map((k) => (
                        <li key={k.id} className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
                            <KeyRound className="size-3.5 text-subtle-foreground" />
                            <span className="font-mono text-xs">
                                {k.key_prefix}…{k.key_suffix}
                            </span>
                            <StatusBadge tone={k.mode === "live" ? "info" : "neutral"}>{k.mode}</StatusBadge>
                            <span className="text-xs text-muted-foreground">
                                {k.revoked_at
                                    ? `Revoked ${new Date(k.revoked_at).toLocaleDateString()}`
                                    : k.last_used_at
                                      ? `Used ${new Date(k.last_used_at).toLocaleString()}`
                                      : "Never used"}
                            </span>
                            {canManage && !k.revoked_at && (
                                <Button size="sm" variant="outline" className="ml-auto" onClick={() => void askRevoke(k)}>
                                    Revoke
                                </Button>
                            )}
                        </li>
                    ))}
                </ul>
            )}
            <SecretOnce
                secret={secret}
                onClose={() => setSecret(null)}
                title="Copy the key now"
                hint="This is the only time the key is shown. Give it to the partner over a secure channel."
            />
        </Panel>
    );
}

function PartnerSecretsCard({ partner: p }: { partner: AdminPartnerDetail }) {
    const confirm = useConfirm();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [secret, setSecret] = useState<string | null>(null);
    const [callbackKey, setCallbackKey] = useState("");
    const rotate = usePartnerWrite(p.id, async () => {
        const r = await rotatePartnerWebhookSecret(p.id);
        setSecret(r.secret);
        return r;
    }, "Webhook secret rotated");
    const saveCallback = usePartnerWrite(p.id, (key: string) => setPartnerCallbackKey(p.id, key), "Callback key saved");

    async function askRotate() {
        if (p.webhook_secret_set) {
            const ok = await confirm({
                title: "Rotate the webhook secret?",
                description: "Events are signed with both the new and the old secret for a day, then with the new one only.",
                confirmLabel: "Rotate",
            });
            if (!ok) return;
        }
        await rotate.mutateAsync(undefined).catch(() => undefined);
    }

    return (
        <Panel title="Secrets" description="Stored encrypted with the instance key and never shown again after they are set.">
            <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                    <div className={FIELD_LABEL}>Webhook signing secret</div>
                    <div className="text-[13px]">
                        {p.webhook_secret_set
                            ? `Set${p.webhook_secret_rotated_at ? `, rotated ${new Date(p.webhook_secret_rotated_at).toLocaleString()}` : ""}`
                            : "Not set: no events are delivered until it is."}
                    </div>
                    {canManage && (
                        <Button size="sm" variant="outline" disabled={rotate.isPending} onClick={() => void askRotate()}>
                            {p.webhook_secret_set ? "Rotate secret" : "Create secret"}
                        </Button>
                    )}
                </div>
                <form
                    className="space-y-1.5"
                    onSubmit={(e) => {
                        e.preventDefault();
                        saveCallback.mutate(callbackKey.trim(), { onSuccess: () => setCallbackKey("") });
                    }}
                >
                    <Label htmlFor="partner-callback-key" className={FIELD_LABEL}>
                        Token callback key {p.callback_key_set ? "(set)" : "(not set)"}
                    </Label>
                    <div className="flex gap-2">
                        <Input
                            id="partner-callback-key"
                            type="password"
                            autoComplete="off"
                            placeholder={p.callback_key_set ? "Enter a new key to replace it" : "The key the partner issued"}
                            value={callbackKey}
                            onChange={(e) => setCallbackKey(e.target.value)}
                            disabled={!canManage}
                        />
                        {canManage && (
                            <Button size="sm" type="submit" disabled={saveCallback.isPending || !callbackKey.trim()}>
                                Save
                            </Button>
                        )}
                    </div>
                </form>
            </div>
            <SecretOnce
                secret={secret}
                onClose={() => setSecret(null)}
                title="Copy the webhook secret now"
                hint="The partner verifies every event with it. This is the only time it is shown."
            />
        </Panel>
    );
}

function PartnerOrganizations({ partnerId }: { partnerId: string }) {
    const q = useInfiniteQuery({
        queryKey: ["admin", "partners", partnerId, "organizations"],
        queryFn: ({ pageParam }) => listPartnerOrganizations(partnerId, pageParam || undefined),
        initialPageParam: "",
        getNextPageParam: (last) => (last.pagination.has_more ? (last.pagination.next_cursor ?? undefined) : undefined),
    });
    const rows = q.data?.pages.flatMap((p) => p.data) ?? [];
    return (
        <Panel title="Workspaces" description="Every workspace the partner manages, live and test, newest first." bodyClassName="p-0">
            <DataTable
                columns={ORG_COLUMNS}
                rows={rows}
                getRowId={(o) => o.id}
                loading={q.isLoading}
                error={q.error}
                onRetry={() => void q.refetch()}
                emptyTitle="No workspaces"
                emptyHint="The partner creates them through its API."
                csvName="partner-workspaces"
                noun="workspaces"
            />
            {q.hasNextPage && (
                <div className="flex justify-center border-t border-border p-2">
                    <Button size="sm" variant="outline" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                        Load more
                    </Button>
                </div>
            )}
        </Panel>
    );
}
