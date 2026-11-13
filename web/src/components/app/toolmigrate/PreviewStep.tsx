// Step three: what the scan found, and what to bring over.

import React from "react";
import { AlertCircleIcon, InfoIcon, Loader2Icon } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import {
    TOOL_MIGRATION_KINDS,
    type ToolCampaignSummary,
    type ToolMigration,
    type ToolMigrationKind,
    type ToolMigrationWorkspace,
    type ToolProviderInfo,
} from "@/lib/api/models/app/toolmigrate/ToolMigration";
import { cn } from "@/lib/utils";
import { withKind, type Choices } from "./choices";
import { CAMPAIGN_STATUS, IN_FLIGHT, KIND_LABEL, fmtCount, isFinishedCampaign, kindHint, sourceName, workspaceKey } from "./labels";

export default function PreviewStep({
    migration,
    info,
    choices,
    onChoices,
}: {
    migration: ToolMigration;
    info: ToolProviderInfo | undefined;
    choices: Choices;
    onChoices: (c: Choices) => void;
}) {
    const label = info?.label ?? migration.label;
    if (migration.status === "scanning") return <Scanning migration={migration} label={label} />;
    if (migration.status === "failed") {
        return (
            <div className="p-4 sm:p-5">
                <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2.5 text-[12.5px] text-red-700 flex items-start gap-2">
                    <AlertCircleIcon className="w-3.5 h-3.5 mt-0.5 shrink-0" />
                    <div className="min-w-0">
                        <p className="font-medium">The scan of your {label} account did not finish.</p>
                        {migration.error && <p className="mt-0.5 text-red-700/90">{migration.error}</p>}
                        <p className="mt-1 text-red-700/90">Go back to check the key and try again.</p>
                    </div>
                </div>
            </div>
        );
    }

    const has = (k: ToolMigrationKind) => choices.kinds.includes(k);
    const workspaces = migration.inventory ?? [];
    const accounts = (migration.accounts ?? []).filter((a) => a.name || a.email);

    return (
        <div className="p-4 sm:p-5 space-y-5">
            {accounts.length > 0 && (
                <p className="text-[12px] text-slate-500">
                    Connected as{" "}
                    {accounts.map((a, i) => (
                        <React.Fragment key={i}>
                            {i > 0 && ", "}
                            <span className="text-slate-800 font-medium">{a.name || a.email}</span>
                            {a.plan && <span className="text-slate-400"> ({a.plan})</span>}
                        </React.Fragment>
                    ))}
                </p>
            )}

            {(info?.notes?.length ?? 0) > 0 && (
                <ul className="rounded-md border border-slate-200 bg-slate-50/70 px-3 py-2 space-y-1">
                    {info?.notes?.map((n) => (
                        <li key={n} className="text-[12px] text-slate-600 leading-relaxed flex items-start gap-2">
                            <InfoIcon className="w-3 h-3 mt-[3px] shrink-0 text-slate-400" />
                            <span>{n}</span>
                        </li>
                    ))}
                </ul>
            )}

            <section>
                <SectionLabel>Bring over</SectionLabel>
                <div className="rounded-md border border-slate-200 divide-y divide-slate-200/70">
                    {TOOL_MIGRATION_KINDS.map((k) => {
                        const supported = !!info?.kinds.includes(k);
                        const needsCampaigns = k === "leads" && !has("campaigns");
                        const reason = !supported
                            ? `Not available through ${label}'s API. Export it from ${label} and import the CSV instead.`
                            : needsCampaigns
                              ? "Leads come over with their campaigns, so turn campaigns on first."
                              : kindHint(k, info);
                        return (
                            <div
                                key={k}
                                className={cn("px-3 py-2.5 flex items-start gap-3", supported && !needsCampaigns && "cursor-pointer")}
                                onClick={() => supported && !needsCampaigns && onChoices(withKind(choices, k, !has(k)))}
                            >
                                <div className="min-w-0 flex-1">
                                    <div className={cn("text-[12.5px] font-medium", supported ? "text-slate-900" : "text-slate-400")}>{KIND_LABEL[k]}</div>
                                    <div className="text-[11.5px] text-slate-500 leading-snug mt-0.5">{reason}</div>
                                </div>
                                <span onClick={(e) => e.stopPropagation()} className="pt-0.5">
                                    <Toggle
                                        value={supported && has(k)}
                                        disabled={!supported || needsCampaigns}
                                        onChange={(v) => onChoices(withKind(choices, k, v))}
                                        ariaLabel={`Bring over ${KIND_LABEL[k].toLowerCase()}`}
                                    />
                                </span>
                            </div>
                        );
                    })}
                </div>
            </section>

            {(has("campaigns") || has("mailboxes")) && (
                <section>
                    <SectionLabel>Options</SectionLabel>
                    <div className="rounded-md border border-slate-200 divide-y divide-slate-200/70">
                        {has("campaigns") && (
                            <OptionRow
                                title="Skip finished campaigns"
                                hint={`Leaves out campaigns ${label} marks completed or archived.`}
                                value={choices.skipFinished}
                                onChange={(v) => onChoices({ ...choices, skipFinished: v })}
                            />
                        )}
                        {has("mailboxes") && (
                            <OptionRow
                                title="Start warmup on connected mailboxes"
                                hint={`Unless ${label} had warmup turned off for that mailbox.`}
                                value={choices.warmup}
                                onChange={(v) => onChoices({ ...choices, warmup: v })}
                            />
                        )}
                    </div>
                </section>
            )}

            {has("leads") && (
                <section>
                    <SectionLabel>Leads part way through a sequence</SectionLabel>
                    <div role="radiogroup" aria-label="Leads part way through a sequence" className="rounded-md border border-slate-200 divide-y divide-slate-200/70">
                        {IN_FLIGHT.map((o) => {
                            const on = choices.inFlight === o.value;
                            return (
                                <button
                                    key={o.value}
                                    type="button"
                                    role="radio"
                                    aria-checked={on}
                                    onClick={() => onChoices({ ...choices, inFlight: o.value })}
                                    className="w-full px-3 py-2 flex items-start gap-2.5 text-left hover:bg-slate-50/70 transition-colors"
                                >
                                    <span
                                        className={cn(
                                            "mt-0.5 size-3.5 rounded-full border inline-flex items-center justify-center shrink-0",
                                            on ? "border-sky-600" : "border-slate-300",
                                        )}
                                    >
                                        {on && <span className="size-1.5 rounded-full bg-sky-600" />}
                                    </span>
                                    <span className="min-w-0">
                                        <span className="block text-[12.5px] text-slate-900">{o.label}</span>
                                        <span className="block text-[11.5px] text-slate-500 leading-snug">{o.hint}</span>
                                    </span>
                                </button>
                            );
                        })}
                    </div>
                    <p className="text-[11.5px] text-slate-500 mt-1.5 leading-relaxed">
                        Leads who replied, bounced, unsubscribed or finished the sequence are imported as contacts and never enrolled.
                    </p>
                </section>
            )}

            {has("campaigns") && <DraftsNotice label={label} />}

            <section className="space-y-3">
                <SectionLabel>{workspaces.length > 1 ? `Found in ${workspaces.length} workspaces` : "Found"}</SectionLabel>
                {workspaces.length === 0 && <p className="text-[12px] text-slate-500">The scan found nothing to bring over.</p>}
                {workspaces.map((w) => (
                    <WorkspaceCard
                        key={workspaceKey(w.workspace)}
                        ws={w}
                        fallback={label}
                        showCampaigns={has("campaigns")}
                        choices={choices}
                        onChoices={onChoices}
                    />
                ))}
            </section>
        </div>
    );
}

export function DraftsNotice({ label }: { label: string }) {
    return (
        <div className="rounded-md border border-amber-200/80 bg-amber-50/70 px-3 py-2 text-[12px] text-amber-800 leading-relaxed flex items-start gap-2">
            <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0 text-amber-500" />
            <span>
                Imported campaigns are drafts and never launch on their own. Pause each one in {label} before you launch it here, so nobody is emailed twice.
            </span>
        </div>
    );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
    return <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">{children}</div>;
}

function OptionRow({ title, hint, value, onChange }: { title: string; hint: string; value: boolean; onChange: (v: boolean) => void }) {
    return (
        <div className="px-3 py-2.5 flex items-start gap-3 cursor-pointer" onClick={() => onChange(!value)}>
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] text-slate-900">{title}</div>
                <div className="text-[11.5px] text-slate-500 leading-snug mt-0.5">{hint}</div>
            </div>
            <span onClick={(e) => e.stopPropagation()} className="pt-0.5">
                <Toggle value={value} onChange={onChange} ariaLabel={title} />
            </span>
        </div>
    );
}

function Scanning({ migration, label }: { migration: ToolMigration; label: string }) {
    const found = migration.inventory ?? [];
    return (
        <div className="p-4 sm:p-5">
            <div className="rounded-md border border-slate-200 px-4 py-8 flex flex-col items-center text-center">
                <Loader2Icon className="w-5 h-5 text-sky-600 animate-spin" />
                <p className="mt-3 text-[13px] font-medium text-slate-900">Reading your {label} account</p>
                <p className="mt-1 text-[12px] text-slate-500 max-w-[44ch] leading-relaxed">
                    Counting mailboxes, campaigns, leads and blocked addresses. Nothing is brought over until you choose.
                </p>
                {found.length > 0 && (
                    <p className="mt-3 text-[11.5px] text-slate-500 tabular-nums">
                        {found.length} {found.length === 1 ? "workspace" : "workspaces"} read so far
                    </p>
                )}
            </div>
        </div>
    );
}

function WorkspaceCard({
    ws,
    fallback,
    showCampaigns,
    choices,
    onChoices,
}: {
    ws: ToolMigrationWorkspace;
    fallback: string;
    showCampaigns: boolean;
    choices: Choices;
    onChoices: (c: Choices) => void;
}) {
    const inv = ws.inventory;
    const summaries = inv.campaign_summaries ?? [];
    return (
        <div className="rounded-md border border-slate-200 overflow-hidden">
            <div className="px-3 h-9 flex items-center gap-2 border-b border-slate-200/70 bg-slate-50/60">
                <span className="text-[12.5px] font-medium text-slate-900 truncate">{sourceName(ws.workspace.name, fallback)}</span>
            </div>
            {ws.error ? (
                <div role="alert" className="px-3 py-2.5 text-[12px] text-red-700 flex items-start gap-2">
                    <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                    <span className="min-w-0">This workspace could not be read: {ws.error}</span>
                </div>
            ) : (
                <>
                    <div className="grid grid-cols-2 sm:grid-cols-4 gap-px bg-slate-200/70 border-b border-slate-200/70">
                        <Count label="Mailboxes" value={inv.mailboxes} />
                        <Count label="Campaigns" value={inv.campaigns} />
                        <Count label="Leads" value={inv.leads} />
                        <Count label="Blocklist" value={inv.blocklist} />
                    </div>
                    {showCampaigns && summaries.length > 0 && (
                        <CampaignTable summaries={summaries} choices={choices} onChoices={onChoices} />
                    )}
                </>
            )}
        </div>
    );
}

function Count({ label, value }: { label: string; value: number }) {
    return (
        <div className="px-3 py-2 bg-white">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{label}</div>
            <div className="text-[14px] font-semibold text-slate-900 tabular-nums mt-0.5" title={value < 0 ? "Not counted by this tool's API" : undefined}>
                {fmtCount(value)}
            </div>
        </div>
    );
}

function CampaignTable({ summaries, choices, onChoices }: { summaries: ToolCampaignSummary[]; choices: Choices; onChoices: (c: Choices) => void }) {
    const pickable = summaries.filter((s) => !(choices.skipFinished && isFinishedCampaign(s.status)));
    const picked = pickable.filter((s) => !choices.excluded.includes(s.id));
    const all = pickable.length > 0 && picked.length === pickable.length;
    const some = picked.length > 0 && !all;

    const setAll = (on: boolean) => {
        const ids = new Set(pickable.map((s) => s.id));
        const rest = choices.excluded.filter((id) => !ids.has(id));
        onChoices({ ...choices, excluded: on ? rest : [...rest, ...ids] });
    };
    const toggle = (id: string) =>
        onChoices({
            ...choices,
            excluded: choices.excluded.includes(id) ? choices.excluded.filter((x) => x !== id) : [...choices.excluded, id],
        });

    return (
        <div>
            <div className="h-8 px-3 flex items-center gap-2.5 border-b border-slate-200/70 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                <Checkbox
                    checked={all}
                    ref={(el) => {
                        if (el) el.indeterminate = some;
                    }}
                    disabled={pickable.length === 0}
                    onChange={(e) => setAll(e.target.checked)}
                    aria-label="Bring over every campaign in this workspace"
                />
                <span className="flex-1 min-w-0">Campaign</span>
                <span className="w-20 hidden sm:block">Status</span>
                <span className="w-12 text-right hidden sm:block">Steps</span>
                <span className="w-16 text-right">Leads</span>
            </div>
            <div className="max-h-64 overflow-y-auto divide-y divide-slate-200/60">
                {summaries.map((s) => {
                    const skipped = choices.skipFinished && isFinishedCampaign(s.status);
                    const on = !skipped && !choices.excluded.includes(s.id);
                    return (
                        <label
                            key={s.id}
                            className={cn("min-h-9 px-3 py-1.5 flex items-center gap-2.5 text-[12.5px]", skipped ? "text-slate-400" : "cursor-pointer hover:bg-slate-50/70")}
                        >
                            <Checkbox checked={on} disabled={skipped} onChange={() => toggle(s.id)} aria-label={`Bring over ${s.name}`} />
                            <span className="flex-1 min-w-0">
                                <span className={cn("block truncate", skipped ? "text-slate-400" : "text-slate-900")}>{s.name}</span>
                                <span className="block sm:hidden text-[11px] text-slate-500">
                                    {CAMPAIGN_STATUS[s.status] ?? s.status}
                                    {s.steps >= 0 && `, ${s.steps} ${s.steps === 1 ? "step" : "steps"}`}
                                    {skipped && ", skipped as finished"}
                                </span>
                                {skipped && <span className="hidden sm:block text-[11px] text-slate-400">Skipped as finished</span>}
                            </span>
                            <span className="w-20 hidden sm:block text-[12px] text-slate-600">{CAMPAIGN_STATUS[s.status] ?? s.status}</span>
                            <span className="w-12 text-right hidden sm:block tabular-nums text-slate-600">{fmtCount(s.steps)}</span>
                            <span className="w-16 text-right tabular-nums text-slate-600">{fmtCount(s.leads)}</span>
                        </label>
                    );
                })}
            </div>
        </div>
    );
}
