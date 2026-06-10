// SyncSourceEditDrawer — edit a saved sync source's options without re-running
// the column mapper. Editing the sheet/tab/mapping is a "make a new source"
// operation conceptually, so here we only expose the safe, common edits:
// label, dedup, target campaign (with detach), categories, and the header flag.
// Sheet/tab/mapping are shown read-only for context.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast";

import { DEDUP_OPTIONS, describeError } from "./importShared";
import CategoryPicker from "./CategoryPicker";
import { Label, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useUpdateLeadSyncSource from "@/lib/api/hooks/app/leadsync/useUpdateLeadSyncSource";
import type {
    ImportDedupStrategy,
    LeadSyncSource,
    UpdateLeadSyncSource,
} from "@/lib/api/models/app/leadsync/LeadSync";

export default function SyncSourceEditDrawer({
    source,
    onClose,
}: {
    source: LeadSyncSource;
    onClose: () => void;
}) {
    const update = useUpdateLeadSyncSource();
    const [query, setQuery] = React.useState("");
    const campaigns = useCampaigns({ query, folder: "" });

    const [label, setLabel] = React.useState(source.label ?? "");
    const [dedup, setDedup] = React.useState<ImportDedupStrategy>(source.dedup);
    const [hasHeader, setHasHeader] = React.useState(source.has_header);
    const [campaignId, setCampaignId] = React.useState<string | null>(
        source.target_campaign_id ?? null,
    );
    const [categoryIds, setCategoryIds] = React.useState<string[]>(source.category_ids ?? []);
    const [busy, setBusy] = React.useState(false);

    const campaignName =
        campaigns.campaigns.find((c) => c.id === campaignId)?.name ??
        (campaignId ? "Selected campaign" : "No campaign");

    async function save() {
        setBusy(true);
        try {
            const body: UpdateLeadSyncSource = {
                label: label.trim() || source.sheet_title || "Sync source",
                dedup,
                has_header: hasHeader,
                category_ids: categoryIds,
            };
            // A nil pointer can't express "clear", so detach explicitly.
            if (campaignId) {
                body.target_campaign_id = campaignId;
            } else if (source.target_campaign_id) {
                body.clear_campaign = true;
            }
            await update.mutateAsync({ id: source.id, body });
            toast.success("Sync source updated");
            onClose();
        } catch (err) {
            toast.error(describeError(err, "Couldn't update the sync source."));
        } finally {
            setBusy(false);
        }
    }

    return (
        <AnimatePresence>
            <motion.div
                key="overlay"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.15 }}
                className="fixed inset-0 z-[130] flex"
            >
                <button
                    type="button"
                    aria-label="Close"
                    onClick={onClose}
                    className="absolute inset-0 bg-slate-900/30 backdrop-blur-[2px]"
                />
                <motion.div
                    key="panel"
                    initial={{ x: 24, opacity: 0 }}
                    animate={{ x: 0, opacity: 1 }}
                    exit={{ x: 24, opacity: 0 }}
                    transition={{ duration: 0.18 }}
                    className="ml-auto h-full w-full max-w-full md:w-[440px] md:max-w-[92vw] bg-white shadow-xl flex flex-col z-10 relative"
                >
                    <div className="h-12 px-5 border-b border-slate-200 flex items-center gap-3 shrink-0">
                        <div className="min-w-0 flex-1">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Edit sync source
                            </div>
                            <div className="text-[12.5px] text-slate-900 font-medium truncate">
                                {source.label || source.sheet_title || "Sync source"}
                            </div>
                        </div>
                        <button
                            type="button"
                            onClick={onClose}
                            aria-label="Close"
                            className="h-7 w-7 rounded border border-slate-200 hover:border-slate-300 text-slate-500 hover:text-slate-900 inline-flex items-center justify-center transition-colors"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </div>

                    <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">
                        <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3 space-y-1">
                            <Row label="Spreadsheet" value={source.sheet_title || source.sheet_id} />
                            {source.tab_title && <Row label="Tab" value={source.tab_title} />}
                            <Row
                                label="Mapped columns"
                                value={`${source.column_mapping.filter((m) => m.target !== "ignore").length} fields`}
                            />
                        </div>

                        <section>
                            <Label>Source label</Label>
                            <TextInput value={label} onChange={setLabel} placeholder="My leads sheet" />
                        </section>

                        <section>
                            <label className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-700 cursor-pointer">
                                <input
                                    type="checkbox"
                                    className="w-3.5 h-3.5 rounded accent-slate-900"
                                    checked={hasHeader}
                                    onChange={(e) => setHasHeader(e.target.checked)}
                                />
                                First row is a header
                            </label>
                        </section>

                        <section>
                            <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                                Duplicate handling
                            </h2>
                            <div className="space-y-2">
                                {DEDUP_OPTIONS.map((opt) => (
                                    <label
                                        key={opt.id}
                                        className={`block rounded-md border p-2.5 cursor-pointer transition-colors ${
                                            dedup === opt.id
                                                ? "border-slate-900 bg-slate-50"
                                                : "border-slate-200 hover:border-slate-300"
                                        }`}
                                    >
                                        <div className="flex items-start gap-2">
                                            <input
                                                type="radio"
                                                name="leadsync-edit-dedup"
                                                className="mt-0.5 accent-slate-900"
                                                checked={dedup === opt.id}
                                                onChange={() => setDedup(opt.id)}
                                            />
                                            <div className="flex-1 min-w-0">
                                                <div className="text-[12px] font-medium text-slate-900 leading-tight">
                                                    {opt.label}
                                                </div>
                                                <div className="text-[11px] text-slate-500 leading-snug mt-0.5">
                                                    {opt.hint}
                                                </div>
                                            </div>
                                        </div>
                                    </label>
                                ))}
                            </div>
                        </section>

                        <section>
                            <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                                Enroll in campaign
                            </h2>
                            <PopoverMenu align="start">
                                <PopoverMenuTrigger asChild>
                                    <SelectButton label={campaignName} className="w-full" />
                                </PopoverMenuTrigger>
                                <PopoverMenuContent minWidth={280}>
                                    <div className="px-2 py-1.5 border-b border-slate-200">
                                        <input
                                            value={query}
                                            onChange={(e) => setQuery(e.target.value)}
                                            placeholder="Search campaigns…"
                                            className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                                        />
                                    </div>
                                    <PopoverMenuItem
                                        selected={!campaignId}
                                        onSelect={() => setCampaignId(null)}
                                    >
                                        No campaign
                                    </PopoverMenuItem>
                                    {campaigns.campaigns.map((c) => (
                                        <PopoverMenuItem
                                            key={c.id}
                                            selected={c.id === campaignId}
                                            onSelect={() => setCampaignId(c.id)}
                                        >
                                            {c.name}
                                        </PopoverMenuItem>
                                    ))}
                                </PopoverMenuContent>
                            </PopoverMenu>
                        </section>

                        <section>
                            <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                                Apply categories
                            </h2>
                            <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
                        </section>
                    </div>

                    <div className="mt-auto border-t border-slate-200 px-5 py-3 flex items-center justify-end gap-2 shrink-0">
                        <button
                            type="button"
                            onClick={onClose}
                            className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 hover:text-slate-900 transition-colors"
                        >
                            Cancel
                        </button>
                        <button
                            type="button"
                            onClick={save}
                            disabled={busy}
                            className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                            Save changes
                        </button>
                    </div>
                </motion.div>
            </motion.div>
        </AnimatePresence>
    );
}

function Row({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-center justify-between gap-3">
            <span className="text-[10.5px] uppercase tracking-[0.1em] text-slate-400">{label}</span>
            <span className="text-[12px] text-slate-700 truncate">{value}</span>
        </div>
    );
}
