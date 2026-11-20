// Import: saved Salesforce sources (list views and Campaigns) that bring people
// into Warmbly, once or every 30 minutes.

import React from "react";
import {
    AlertTriangleIcon,
    DownloadCloudIcon,
    Loader2Icon,
    MoreHorizontalIcon,
    PauseIcon,
    PencilIcon,
    PlayIcon,
    PlusIcon,
    RefreshCwIcon,
    Trash2Icon,
} from "lucide-react";
import toast from "react-hot-toast/headless";

import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { CategoryChip } from "@/components/app/contacts/CategoryPicker";
import { SegmentChips } from "@/components/app/segments/SegmentPickers";
import { useConfirm } from "@/hooks/context/confirm";
import { useUserProfile } from "@/hooks/context/user";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import {
    useDeleteSalesforceImportSource,
    useRunSalesforceImportSource,
    useSalesforceImportSources,
    useUpdateSalesforceImportSource,
} from "@/lib/api/hooks/app/integrations/useSalesforce";
import {
    SALESFORCE_IMPORT_OBJECT_LABELS,
    type SalesforceImportSource,
} from "@/lib/api/models/app/integrations/Salesforce";
import { cn } from "@/lib/utils";

import ImportDialog, { EditImportDialog } from "./ImportDialog";
import { Pill, primaryBtn } from "./shared";
import { absolute, ago, errMsg } from "./util";

export default function ImportTab({ connectionId }: { connectionId: string }) {
    const sources = useSalesforceImportSources(connectionId);
    const [creating, setCreating] = React.useState(false);
    const [editing, setEditing] = React.useState<SalesforceImportSource | null>(null);
    const campaigns = useCampaigns({ query: "", folder: "" });
    const campaignName = React.useCallback(
        (id?: string | null) => (id ? (campaigns.campaigns.find((c) => c.id === id)?.name ?? "A campaign") : null),
        [campaigns.campaigns],
    );

    const list = sources.data ?? [];

    return (
        <div className="space-y-4 max-w-5xl">
            <div className="flex flex-wrap items-center gap-3">
                <p className="text-[12px] text-slate-600 flex-1 min-w-[240px]">
                    Bring people in from a Salesforce list view or Campaign. Records stay linked, so activity logs back to
                    them.
                </p>
                <button type="button" onClick={() => setCreating(true)} className={primaryBtn}>
                    <PlusIcon className="w-3.5 h-3.5" />
                    New import
                </button>
            </div>

            {sources.isPending ? (
                <div className="space-y-2">
                    {[0, 1].map((i) => (
                        <div key={i} className="h-20 rounded-md bg-slate-100 animate-pulse" />
                    ))}
                </div>
            ) : sources.isError ? (
                <div className="rounded-md border border-rose-200 bg-rose-50 px-4 py-3 text-[12px] text-rose-700">
                    {errMsg(sources.error, "Could not load imports")}
                </div>
            ) : list.length === 0 ? (
                <div className="rounded-md border border-dashed border-slate-300 px-6 py-10 text-center">
                    <DownloadCloudIcon className="w-5 h-5 text-slate-300 mx-auto" />
                    <p className="text-[12.5px] font-medium text-slate-700 mt-2">No imports yet</p>
                    <p className="text-[11.5px] text-slate-500 mt-0.5">
                        Import a list view like “My open leads” or a Salesforce Campaign, and keep it in sync.
                    </p>
                    <button type="button" onClick={() => setCreating(true)} className={cn(primaryBtn, "mt-3")}>
                        <PlusIcon className="w-3.5 h-3.5" />
                        New import
                    </button>
                </div>
            ) : (
                <div className="rounded-md border border-slate-200 bg-white divide-y divide-slate-100">
                    {list.map((s) => (
                        <SourceRow
                            key={s.id}
                            connectionId={connectionId}
                            source={s}
                            campaign={campaignName(s.campaign_id)}
                            onEdit={() => setEditing(s)}
                        />
                    ))}
                </div>
            )}

            {creating && <ImportDialog connectionId={connectionId} onClose={() => setCreating(false)} />}
            {editing && (
                <EditImportDialog connectionId={connectionId} source={editing} onClose={() => setEditing(null)} />
            )}
        </div>
    );
}

function SourceRow({
    connectionId,
    source: s,
    campaign,
    onEdit,
}: {
    connectionId: string;
    source: SalesforceImportSource;
    campaign: string | null;
    onEdit: () => void;
}) {
    const confirm = useConfirm();
    const { user } = useUserProfile();
    const run = useRunSalesforceImportSource(connectionId);
    const update = useUpdateSalesforceImportSource(connectionId);
    const del = useDeleteSalesforceImportSource(connectionId);

    const categories = (s.category_ids ?? [])
        .map((id) => (user.categories ?? []).find((c) => c.id === id))
        .filter((c): c is NonNullable<typeof c> => !!c);
    const running = s.status === "running";
    const r = s.last_result;

    function runNow() {
        run.mutate(
            { connectionId, sourceId: s.id },
            {
                onSuccess: () => toast.success("Import started"),
                onError: (err) => toast.error(errMsg(err, "Could not start the import")),
            },
        );
    }
    function toggleEnabled() {
        update.mutate(
            { connectionId, sourceId: s.id, body: { enabled: !s.enabled } },
            {
                onSuccess: () => toast.success(s.enabled ? "Import paused" : "Import resumed"),
                onError: (err) => toast.error(errMsg(err, "Could not update the import")),
            },
        );
    }
    function remove() {
        confirm.show(
            `Delete the import “${s.name}”? Contacts it already brought in stay in Warmbly.`,
            async () => {
                try {
                    await del.mutateAsync({ connectionId, sourceId: s.id });
                    toast.success("Import deleted");
                } catch (err) {
                    toast.error(errMsg(err, "Could not delete the import"));
                    throw err;
                }
            },
        );
    }

    return (
        <div
            role="button"
            tabIndex={0}
            onClick={onEdit}
            onKeyDown={(e) => {
                // Only the row itself: keys from its menu bubble here through the portal.
                if (e.target !== e.currentTarget) return;
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onEdit();
                }
            }}
            className="px-4 py-3 flex items-start gap-3 hover:bg-slate-50/60 transition-colors cursor-pointer outline-none focus-visible:bg-slate-50"
        >
            <div className="min-w-0 flex-1 space-y-1.5">
                <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{s.name}</span>
                    <Pill>{s.source_kind === "campaign" ? "Campaign" : SALESFORCE_IMPORT_OBJECT_LABELS[s.object]}</Pill>
                    {s.recurring && s.enabled && <Pill tone="sky">Syncs every 30 min</Pill>}
                    {!s.enabled && <Pill tone="amber">Paused</Pill>}
                    <StatusBadge status={s.status} />
                </div>
                <div className="text-[11.5px] text-slate-500 flex flex-wrap items-center gap-x-2 gap-y-0.5">
                    <span className="truncate">From {s.source_label}</span>
                    <span className="text-slate-300">·</span>
                    <span>{campaign ? `Adds to ${campaign}` : "Contacts only"}</span>
                    <span className="text-slate-300">·</span>
                    <span title={absolute(s.last_run_at)}>Last run {ago(s.last_run_at)}</span>
                    <span className="text-slate-300">·</span>
                    <span>{s.total_imported.toLocaleString()} imported in total</span>
                </div>
                {(categories.length > 0 || (s.segment_ids?.length ?? 0) > 0) && (
                    <div className="flex flex-wrap gap-1">
                        <SegmentChips ids={s.segment_ids} />
                        {categories.map((c) => (
                            <CategoryChip key={c.id} category={c} compact />
                        ))}
                    </div>
                )}
                {r && (
                    <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-slate-500 tabular-nums">
                        <Count label="read" value={r.read} />
                        <Count label="new" value={r.imported} strong />
                        <Count label="updated" value={r.updated} />
                        <Count label="linked" value={r.linked} />
                        <Count label="skipped" value={r.skipped} />
                        <Count label="without email" value={r.no_email} />
                        <Count label="opted out" value={r.opted_out ?? 0} />
                        {r.failed > 0 && <Count label="failed" value={r.failed} tone="rose" />}
                        {r.truncated && <span className="text-amber-700">Stopped at the per-run limit, continues next run</span>}
                    </div>
                )}
                {s.last_error && (
                    <p className="text-[11px] text-rose-700 flex items-start gap-1.5 break-words">
                        <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                        {s.last_error}
                    </p>
                )}
            </div>
            <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            aria-label={`Actions for ${s.name}`}
                            className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                        >
                            {run.isPending || update.isPending ? (
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                                <MoreHorizontalIcon className="w-3.5 h-3.5" />
                            )}
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={180}>
                        <PopoverMenuItem
                            icon={<RefreshCwIcon className="w-3 h-3" />}
                            onSelect={runNow}
                            disabled={running || !s.enabled}
                        >
                            Run now
                        </PopoverMenuItem>
                        <PopoverMenuItem
                            icon={s.enabled ? <PauseIcon className="w-3 h-3" /> : <PlayIcon className="w-3 h-3" />}
                            onSelect={toggleEnabled}
                        >
                            {s.enabled ? "Pause" : "Resume"}
                        </PopoverMenuItem>
                        <PopoverMenuItem icon={<PencilIcon className="w-3 h-3" />} onSelect={onEdit}>
                            Edit
                        </PopoverMenuItem>
                        <PopoverMenuSeparator />
                        <PopoverMenuItem icon={<Trash2Icon className="w-3 h-3" />} onSelect={remove} danger>
                            Delete
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </div>
        </div>
    );
}

function StatusBadge({ status }: { status: SalesforceImportSource["status"] }) {
    if (status === "running")
        return (
            <Pill tone="sky">
                <Loader2Icon className="w-2.5 h-2.5 animate-spin" />
                Running
            </Pill>
        );
    if (status === "error") return <Pill tone="rose">Error</Pill>;
    return <Pill tone="emerald">Idle</Pill>;
}

function Count({ label, value, strong, tone }: { label: string; value: number; strong?: boolean; tone?: "rose" }) {
    return (
        <span className={cn(tone === "rose" && "text-rose-700")}>
            <span className={cn(strong ? "text-slate-900 font-medium" : "text-slate-700", tone === "rose" && "text-rose-700")}>
                {value.toLocaleString()}
            </span>{" "}
            {label}
        </span>
    );
}
