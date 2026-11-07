// Where this step's copy landed at business inboxes when its senders' warmup
// carried it. Warmup opens some of its threads with a running campaign's copy
// on its own; this strip only reports, and stays hidden until an arm has
// enough receipts for its rate to mean something.

import type { WarmupCopyPlacement, WarmupStepPlacement } from "@/lib/api/models/app/campaigns/WarmupPlacement";
import { useCampaignWarmupPlacement } from "@/lib/api/hooks/app/campaigns/useCampaignWarmupPlacement";

const PROVIDERS: { group: string; label: string }[] = [
    { group: "google", label: "Google Workspace" },
    { group: "microsoft", label: "Microsoft 365" },
];

// Receipts an arm needs before its rates show, and a provider before its own does.
const MIN_ARM_CHECKED = 20;
const MIN_PROVIDER_CHECKED = 10;

const rate = (n: number, of: number) => Math.round((n / of) * 100);

function inboxTone(pct: number) {
    if (pct >= 90) return "text-emerald-600";
    if (pct >= 75) return "text-amber-600";
    return "text-rose-600";
}

export default function StepWarmupPlacement({
    campaignId,
    stepId,
    armName,
}: {
    campaignId: string;
    stepId: string;
    // Display name of an arm by key ("original" or a variant id); undefined hides a row for an arm no longer on the step.
    armName: (key: string) => string | undefined;
}) {
    const { data } = useCampaignWarmupPlacement(campaignId);
    const rows = (data?.data ?? [])
        .filter((r) => r.sequence_id === stepId && r.placement.reported >= MIN_ARM_CHECKED)
        .map((r) => ({ row: r, name: armName(r.variant_id ?? "original") }))
        .filter((r): r is { row: WarmupStepPlacement; name: string } => r.name !== undefined);
    if (rows.length === 0) return null;
    const named = rows.length > 1 || rows[0].row.variant_id !== null;

    return (
        <div className="border-b border-slate-200 px-2 py-2 space-y-1">
            <div className="flex items-baseline gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400">Warmup placement</span>
                <span className="text-[10.5px] text-slate-400">last {data?.days ?? 30} days</span>
            </div>
            {rows.map(({ row, name }) => (
                <div
                    key={row.variant_id ?? "original"}
                    className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11.5px] tabular-nums"
                >
                    {named && <span className="w-20 truncate text-slate-600">{name}</span>}
                    <Headline p={row.placement} />
                    <span className="inline-flex flex-wrap items-center gap-x-2 text-[11px] text-slate-500">
                        {PROVIDERS.map(({ group, label }) => {
                            const g = row.by_group.find((x) => x.group === group);
                            if (!g || g.reported < MIN_PROVIDER_CHECKED) return null;
                            const pct = rate(g.inbox + g.tabs, g.reported);
                            return (
                                <span key={group}>
                                    {label} <span className={`font-medium ${inboxTone(pct)}`}>{pct}%</span>
                                </span>
                            );
                        })}
                    </span>
                    <span className="text-[10.5px] text-slate-400">{row.placement.reported.toLocaleString()} checked</span>
                </div>
            ))}
            <p className="text-[11px] text-slate-400">
                Where this copy landed when your mailboxes' warmup sent it to business inboxes on Google Workspace and Microsoft 365.
            </p>
        </div>
    );
}

function Headline({ p }: { p: WarmupCopyPlacement }) {
    const inbox = rate(p.inbox, p.reported);
    const tabs = rate(p.tabs, p.reported);
    const spam = rate(p.spam, p.reported);
    return (
        <span className="inline-flex items-center gap-2">
            <span>
                <span className={`font-medium ${inboxTone(inbox + tabs)}`}>{inbox}%</span>
                <span className="text-slate-400"> inbox</span>
            </span>
            {tabs > 0 && (
                <span>
                    <span className="font-medium text-slate-600">{tabs}%</span>
                    <span className="text-slate-400"> tabs</span>
                </span>
            )}
            <span>
                <span className={`font-medium ${spam > 10 ? "text-rose-600" : "text-slate-600"}`}>{spam}%</span>
                <span className="text-slate-400"> spam</span>
            </span>
        </span>
    );
}
