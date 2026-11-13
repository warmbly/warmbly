// The last step: progress per target while it runs, then what came over and what is left to do.

import React from "react";
import { Link } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import { AlertCircleIcon, ArrowUpRightIcon, CheckIcon, CopyIcon, Link2Icon, Loader2Icon, PlusIcon, RefreshCwIcon } from "lucide-react";
import { useConfirm } from "@/hooks/context/confirm";
import { useUserProfile } from "@/hooks/context/user";
import { useOpenClient } from "@/lib/api/hooks/app/agency/useAgencyHome";
import { useCreateConnectLink } from "@/lib/api/hooks/app/emails/useConnectLinks";
import { useCancelToolMigration, useLinkToolMigrationSenders } from "@/lib/api/hooks/app/toolmigrate/useToolMigrations";
import type { ConnectLinkProvider } from "@/lib/api/models/app/emails/ConnectLink";
import type {
    ToolMailboxKind,
    ToolMigration,
    ToolMigrationKind,
    ToolMigrationMailbox,
    ToolMigrationScope,
    ToolMigrationTarget,
} from "@/lib/api/models/app/toolmigrate/ToolMigration";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { DraftsNotice } from "./PreviewStep";
import { KIND_LABEL, TARGET_STATUS, plural, sourceName } from "./labels";
import { Pill, StatusPill } from "./parts";

const MAILBOX_KIND: Record<ToolMailboxKind, string> = { google: "Google", microsoft: "Microsoft", smtp_imap: "IMAP / SMTP" };
const LINK_PROVIDER: Record<ToolMailboxKind, ConnectLinkProvider> = { google: "gmail", microsoft: "outlook", smtp_imap: "smtp_imap" };

export default function RunStep({
    scope,
    migration,
    label,
    onLeave,
}: {
    scope: ToolMigrationScope;
    migration: ToolMigration;
    label: string;
    /** Closes the wizard before something else takes the screen. */
    onLeave: () => void;
}) {
    const confirm = useConfirm();
    const cancel = useCancelToolMigration(scope);
    const relink = useLinkToolMigrationSenders(scope);
    const targets = React.useMemo(() => migration.targets ?? [], [migration.targets]);
    const kinds = migration.options?.kinds;
    const running = migration.status === "running";
    const missing = targets.flatMap((t) => t.followup?.campaigns ?? []).filter((c) => (c.missing_senders?.length ?? 0) > 0);

    const askCancel = () =>
        confirm.show(
            `Cancel this migration? What came over so far stays, and the rest is left in ${label}.`,
            async () => {
                try {
                    await cancel.mutateAsync(migration.id);
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );

    const linkAgain = async () => {
        try {
            const m = await relink.mutateAsync(migration.id);
            const left = (m.targets ?? []).flatMap((t) => t.followup?.campaigns ?? []).filter((c) => (c.missing_senders?.length ?? 0) > 0).length;
            toast.success(left ? `Linked what is connected. ${plural(left, "campaign")} still missing senders.` : "Every campaign has its senders now");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    // An agency's targets are grouped by the client they landed in.
    const groups = React.useMemo(() => {
        const by = new Map<string, { orgId: string; name: string; targets: ToolMigrationTarget[] }>();
        for (const t of targets) {
            const g = by.get(t.organization_id) ?? { orgId: t.organization_id, name: t.organization_name || "Client workspace", targets: [] };
            g.targets.push(t);
            by.set(t.organization_id, g);
        }
        return [...by.values()].sort((a, b) => a.name.localeCompare(b.name));
    }, [targets]);

    return (
        <div className="p-4 sm:p-5 space-y-4">
            <div className="flex flex-wrap items-center gap-2">
                <StatusPill status={migration.status} />
                <span className="text-[12.5px] text-slate-600">
                    {running
                        ? `Bringing your ${label} data over. It keeps running if you close this.`
                        : migration.status === "completed"
                          ? `Finished bringing your ${label} data over.`
                          : migration.status === "canceled"
                            ? "Canceled. What came over before that stays."
                            : migration.status === "failed"
                              ? "The migration stopped."
                              : "Starting."}
                </span>
                {running && (
                    <button
                        type="button"
                        onClick={askCancel}
                        disabled={cancel.isPending}
                        className="ml-auto h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 bg-white transition-colors disabled:opacity-60"
                    >
                        Cancel migration
                    </button>
                )}
            </div>

            {migration.error && (
                <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-[12px] text-red-700 flex items-start gap-2">
                    <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                    <span className="min-w-0">{migration.error}</span>
                </div>
            )}

            {(!kinds || kinds.includes("campaigns")) && <DraftsNotice label={label} />}

            {missing.length > 0 && !running && (
                <div className="rounded-md border border-slate-200 px-3 py-2.5 flex flex-col sm:flex-row sm:items-center gap-2">
                    <p className="flex-1 text-[12px] text-slate-600 leading-relaxed">
                        {plural(missing.length, "campaign")} could not find {missing.length === 1 ? "its" : "their"} senders here yet. Connect those mailboxes, then link them again.
                    </p>
                    <button
                        type="button"
                        onClick={linkAgain}
                        disabled={relink.isPending}
                        className="shrink-0 h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {relink.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCwIcon className="w-3 h-3" />}
                        Link senders again
                    </button>
                </div>
            )}

            {targets.length === 0 && <p className="text-[12px] text-slate-500">Nothing has started yet.</p>}

            {scope.kind === "agency"
                ? groups.map((g) => <ClientGroup key={g.orgId} group={g} migration={migration} label={label} kinds={kinds} onLeave={onLeave} />)
                : targets.map((t) => (
                      <TargetCard
                          key={t.id}
                          target={t}
                          heading={targets.length > 1 ? sourceName(t.workspace.name, label) : undefined}
                          kinds={kinds}
                          campaignLink={(c) => (
                              <Link
                                  to="/app/campaigns/$id"
                                  params={{ id: c.campaign_id }}
                                  onClick={onLeave}
                                  className="text-slate-900 hover:text-sky-700 hover:underline underline-offset-2 truncate"
                              >
                                  {c.name}
                              </Link>
                          )}
                          reconnect={<WorkspaceReconnect migration={migration} target={t} label={label} onLeave={onLeave} />}
                      />
                  ))}
        </div>
    );
}

function ClientGroup({
    group,
    migration,
    label,
    kinds,
    onLeave,
}: {
    group: { orgId: string; name: string; targets: ToolMigrationTarget[] };
    migration: ToolMigration;
    label: string;
    kinds?: ToolMigrationKind[];
    onLeave: () => void;
}) {
    const { open, pending } = useOpenClient();
    const enter = (then?: Parameters<typeof open>[1]) => {
        void open(group.orgId, then).then((ok) => ok && onLeave());
    };
    return (
        <section className="space-y-2">
            <div className="flex items-center gap-2">
                <span className="text-[13px] font-semibold text-slate-900 truncate">{group.name}</span>
                <button
                    type="button"
                    onClick={() => enter()}
                    disabled={pending}
                    className="ml-auto h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    Open client
                    <ArrowUpRightIcon className="w-3 h-3" />
                </button>
            </div>
            {group.targets.map((t) => (
                <TargetCard
                    key={t.id}
                    target={t}
                    heading={`From ${sourceName(t.workspace.name, migration.label || label)}`}
                    kinds={kinds}
                    campaignLink={(c) => (
                        <button
                            type="button"
                            disabled={pending}
                            onClick={() => enter((nav) => nav({ to: "/app/campaigns/$id", params: { id: c.campaign_id } }))}
                            className="text-left text-slate-900 hover:text-sky-700 hover:underline underline-offset-2 truncate"
                        >
                            {c.name}
                        </button>
                    )}
                    reconnect={
                        <button
                            type="button"
                            disabled={pending}
                            onClick={() => enter((nav) => nav({ to: "/app/emails" }))}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            Open the client&apos;s mailboxes
                            <ArrowUpRightIcon className="w-3 h-3" />
                        </button>
                    }
                />
            ))}
        </section>
    );
}

function TargetCard({
    target: t,
    heading,
    kinds,
    campaignLink,
    reconnect,
}: {
    target: ToolMigrationTarget;
    heading?: string;
    kinds?: ToolMigrationKind[];
    campaignLink: (c: { campaign_id: string; name: string }) => React.ReactNode;
    reconnect: React.ReactNode;
}) {
    const c = t.counts;
    const has = (k: ToolMigrationKind) => !kinds || kinds.includes(k);
    const status = TARGET_STATUS[t.status] ?? { label: t.status, tone: "bg-slate-100 text-slate-500" };
    const campaigns = t.followup?.campaigns ?? [];
    const reconnectList = t.followup?.reconnect ?? [];
    const notes = t.followup?.notes ?? [];

    const stats: { label: string; value: number; sub?: string }[] = [];
    if (has("blocklist")) stats.push({ label: "Blocked", value: c.blocklist_imported });
    if (has("mailboxes")) stats.push({ label: "Mailboxes", value: c.mailboxes_queued + c.mailboxes_manual, sub: c.mailboxes_manual ? `${c.mailboxes_manual} to reconnect` : undefined });
    if (has("campaigns")) stats.push({ label: "Campaigns", value: c.campaigns_created, sub: c.campaigns_skipped ? `${c.campaigns_skipped} skipped` : undefined });
    if (has("leads")) {
        stats.push({ label: "Contacts", value: c.contacts_imported + c.contacts_updated, sub: c.contacts_updated ? `${c.contacts_updated} updated` : undefined });
        stats.push({ label: "Enrolled", value: c.leads_enrolled });
        stats.push({
            label: "Not enrolled",
            value: c.leads_not_enrolled + c.leads_suppressed,
            sub: c.leads_suppressed ? `${c.leads_suppressed} suppressed` : undefined,
        });
        if (c.leads_failed) stats.push({ label: "Failed", value: c.leads_failed });
    }

    return (
        <div className="rounded-md border border-slate-200 overflow-hidden">
            <div className="px-3 min-h-9 py-1.5 flex flex-wrap items-center gap-2 border-b border-slate-200/70 bg-slate-50/60">
                {heading && <span className="text-[12.5px] font-medium text-slate-900 truncate">{heading}</span>}
                <Pill tone={status.tone}>{status.label}</Pill>
                {t.status === "running" && t.phase && (
                    <span className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-500">
                        <Loader2Icon className="w-3 h-3 animate-spin text-sky-600" />
                        Importing {KIND_LABEL[t.phase]?.toLowerCase() ?? t.phase}
                    </span>
                )}
            </div>
            {stats.length > 0 && (
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-px bg-slate-200/70 border-b border-slate-200/70">
                    {stats.map((s) => (
                        <div key={s.label} className="px-3 py-2 bg-white">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{s.label}</div>
                            <div className={cn("text-[14px] font-semibold tabular-nums mt-0.5", s.label === "Failed" ? "text-red-600" : "text-slate-900")}>
                                {s.value.toLocaleString()}
                            </div>
                            {s.sub && <div className="text-[11px] text-slate-500">{s.sub}</div>}
                        </div>
                    ))}
                    {/* White fillers close the last row, so the gap colour never shows as a grey block. */}
                    {Array.from({ length: (4 - (stats.length % 4)) % 4 }, (_, i) => (
                        <div key={`fill-${i}`} className={cn("bg-white", i >= (2 - (stats.length % 2)) % 2 && "hidden sm:block")} />
                    ))}
                </div>
            )}
            <div className="px-3 py-2.5 space-y-3">
                {t.error && (
                    <p role="alert" className="text-[12px] text-red-700 flex items-start gap-1.5">
                        <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                        <span className="min-w-0">{t.error}</span>
                    </p>
                )}
                {t.status === "pending" && <p className="text-[12px] text-slate-500">Waiting its turn.</p>}

                {campaigns.length > 0 && (
                    <div>
                        <SubLabel>Draft campaigns to review</SubLabel>
                        <ul className="space-y-1">
                            {campaigns.map((cp) => (
                                <li key={cp.campaign_id} className="text-[12.5px] min-w-0">
                                    <div className="flex items-center gap-2 min-w-0">{campaignLink(cp)}</div>
                                    {(cp.missing_senders?.length ?? 0) > 0 && (
                                        <p className="text-[11.5px] text-amber-700 leading-snug">
                                            No mailbox here yet for {cp.missing_senders?.join(", ")}
                                        </p>
                                    )}
                                </li>
                            ))}
                        </ul>
                    </div>
                )}

                {reconnectList.length > 0 && (
                    <div>
                        <SubLabel>Mailboxes to connect again</SubLabel>
                        <p className="text-[11.5px] text-slate-500 mb-1.5 leading-relaxed">
                            These sign in with Google or Microsoft, or their password could not be read, so their owner connects them here.
                        </p>
                        <ul className="mb-2 flex flex-wrap gap-1.5">
                            {reconnectList.map((mb) => (
                                <li key={mb.email} className="h-6 px-2 rounded-md bg-slate-100 text-[11.5px] text-slate-700 inline-flex items-center gap-1.5 max-w-full">
                                    <span className="truncate">{mb.email}</span>
                                    <span className="text-slate-400 shrink-0">{MAILBOX_KIND[mb.kind] ?? mb.kind}</span>
                                </li>
                            ))}
                        </ul>
                        {reconnect}
                    </div>
                )}

                {c.mailboxes_queued > 0 && (
                    <p className="text-[12px] text-slate-500">
                        {plural(c.mailboxes_queued, "mailbox", "mailboxes")} with saved passwords {c.mailboxes_queued === 1 ? "is" : "are"} connecting through a mailbox import. Follow it on the Mailboxes page.
                    </p>
                )}

                {notes.length > 0 && (
                    <div>
                        <SubLabel>Did not carry over</SubLabel>
                        <ul className="space-y-0.5">
                            {notes.map((n, i) => (
                                <li key={i} className="text-[12px] text-slate-600 leading-relaxed">
                                    {n}
                                </li>
                            ))}
                        </ul>
                    </div>
                )}
            </div>
        </div>
    );
}

function SubLabel({ children }: { children: React.ReactNode }) {
    return <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">{children}</div>;
}

/** In the workspace itself: a client connect link for the owners, or the add-mailbox dialog. */
function WorkspaceReconnect({ migration, target, label, onLeave }: { migration: ToolMigration; target: ToolMigrationTarget; label: string; onLeave: () => void }) {
    const create = useCreateConnectLink();
    const user = useUserProfile();
    const [url, setUrl] = React.useState<string | null>(null);
    const [copied, setCopied] = React.useState(false);
    const key = React.useRef(crypto.randomUUID());
    const list: ToolMigrationMailbox[] = target.followup?.reconnect ?? [];

    const makeLink = async () => {
        const providers = [...new Set(list.map((m) => LINK_PROVIDER[m.kind]).filter(Boolean))];
        try {
            const out = await create.mutateAsync({
                body: {
                    label: `Reconnect from ${label}`.slice(0, 64),
                    providers: providers.length ? providers : ["gmail", "outlook", "smtp_imap"],
                    max_mailboxes: Math.min(500, Math.max(1, list.length)),
                    expires_in_days: 7,
                    tag_ids: [],
                    warmup: migration.options?.warmup ?? true,
                },
                idempotencyKey: key.current,
            });
            setUrl(out.url);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const copy = async () => {
        if (!url) return;
        try {
            await navigator.clipboard.writeText(url);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
        } catch {
            toast.error("Copy did not work. Select the link and copy it by hand.");
        }
    };

    if (url) {
        return (
            <div className="space-y-1.5">
                <div className="flex items-center gap-1.5">
                    <input
                        readOnly
                        value={url}
                        onFocus={(e) => e.currentTarget.select()}
                        aria-label="Connect link"
                        className="h-7 flex-1 min-w-0 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[16px] md:text-[12px] font-mono text-slate-700 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                    <button
                        type="button"
                        onClick={copy}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 shrink-0"
                    >
                        {copied ? <CheckIcon className="w-3 h-3" /> : <CopyIcon className="w-3 h-3" />}
                        {copied ? "Copied" : "Copy"}
                    </button>
                </div>
                <p className="text-[11.5px] text-slate-500">Send it to the mailbox owners. It works for 7 days and is shown only once.</p>
            </div>
        );
    }

    return (
        <div className="flex flex-wrap gap-1.5">
            <button
                type="button"
                onClick={makeLink}
                disabled={create.isPending}
                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {create.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <Link2Icon className="w-3 h-3" />}
                Create a connect link
            </button>
            <button
                type="button"
                onClick={() => {
                    onLeave();
                    user.setAddEmail(true);
                }}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
            >
                <PlusIcon className="w-3 h-3" />
                Connect them myself
            </button>
        </div>
    );
}
