// The workspace's warmup templates: list with placement, a row menu, and a
// detail drawer with the per-provider breakdown.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { FileTextIcon, MoreHorizontalIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { EmptyBlock, SectionBar } from "@/components/layout/Page";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useDeleteWarmupTemplate, useWarmupTemplate, useWarmupTemplateStats, useWarmupTemplates } from "@/lib/api/hooks/app/warmup/useWarmupCopy";
import type { WarmupTemplate } from "@/lib/api/models/app/warmup/WarmupCopy";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { fmtDate } from "@/components/app/placement/tests/placementTests";
import { CopyPlacementSummary, GroupBreakdown, HEADLINE_NOTE } from "./copyParts";

const EMPTY_PLACEMENT = { sent: 0, reported: 0, inbox: 0, tabs: 0, spam: 0, rescued: 0 };

// Returns the confirm-then-delete action shared by the row menu and the drawer.
function useAskDelete(onDeleted?: () => void) {
    const confirm = useConfirm();
    const remove = useDeleteWarmupTemplate();
    return (t: WarmupTemplate) =>
        confirm.show(`Delete "${t.name || t.subject}"? Warmup stops sending it, and its placement history leaves this page.`, async () => {
            try {
                await remove.mutateAsync(t.id);
                onDeleted?.();
                toast.success("Template deleted");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
}

export default function TemplatesSection({ days, onNew }: { days: number; onNew: () => void }) {
    const list = useWarmupTemplates(days);
    const [openId, setOpenId] = React.useState<string | null>(null);
    const opened = list.templates.find((t) => t.id === openId) ?? null;
    const askDelete = useAskDelete();

    return (
        <section>
            <SectionBar label="Templates" count={list.data ? list.templates.length : undefined} />

            {list.isLoading ? (
                <div className="divide-y divide-slate-200/60">
                    {[0, 1, 2].map((i) => (
                        <div key={i} className="h-12 px-5 flex items-center gap-4">
                            <div className="h-3 w-40 rounded bg-slate-100 animate-pulse" />
                            <div className="h-3 flex-1 rounded bg-slate-50 animate-pulse" />
                        </div>
                    ))}
                </div>
            ) : list.isError ? (
                <EmptyBlock title="Templates could not be loaded" body={buildError(list.error as unknown as AppError)} />
            ) : list.templates.length === 0 ? (
                <EmptyBlock
                    title="No warmup templates yet"
                    body="Add a subject and body you plan to send, and warmup carries it so you see where it lands before prospects get it."
                    cta={
                        <button
                            type="button"
                            onClick={onNew}
                            className="h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium bg-sky-600 hover:bg-sky-700 text-white transition-colors"
                        >
                            <PlusIcon className="w-3.5 h-3.5" />
                            New template
                        </button>
                    }
                />
            ) : (
                <div className="divide-y divide-slate-200/60 border-b border-slate-200/60">
                    {list.templates.map((t) => (
                        <TemplateRow key={t.id} template={t} onOpen={() => setOpenId(t.id)} onDelete={() => askDelete(t)} />
                    ))}
                </div>
            )}

            <AnimatePresence>
                {opened && <TemplateDrawer key={opened.id} template={opened} days={days} onClose={() => setOpenId(null)} />}
            </AnimatePresence>
        </section>
    );
}

function TemplateRow({ template, onOpen, onDelete }: { template: WarmupTemplate; onOpen: () => void; onDelete: () => void }) {
    return (
        <div
            role="button"
            tabIndex={0}
            onClick={onOpen}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onOpen();
                }
            }}
            className="min-h-12 px-5 py-2 flex flex-col md:flex-row md:items-center gap-1.5 md:gap-4 cursor-pointer hover:bg-slate-50/80 transition-colors outline-none focus-visible:bg-slate-50"
        >
            <div className="flex items-center gap-2 min-w-0 md:flex-1">
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 min-w-0">
                        <span className="text-[12.5px] font-medium text-slate-900 truncate">{template.name || template.subject}</span>
                        {template.segment && <SegmentChip segment={template.segment} />}
                    </div>
                    <div className="text-[11.5px] text-slate-500 truncate">{template.subject}</div>
                </div>
                <RowMenu onDelete={onDelete} className="md:hidden" />
            </div>
            <div className="md:w-[46%] min-w-0 flex items-center gap-2">
                <CopyPlacementSummary placement={template.placement ?? EMPTY_PLACEMENT} className="flex-1" />
                <RowMenu onDelete={onDelete} className="hidden md:inline-flex" />
            </div>
        </div>
    );
}

// Keys and clicks inside the menu (which portals, but still bubbles in React) must not open the row.
function RowMenu({ onDelete, className }: { onDelete: () => void; className?: string }) {
    return (
        <span className={`shrink-0 ${className ?? ""}`} onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
            <PopoverMenu align="end">
                <PopoverMenuTrigger asChild>
                    <button
                        type="button"
                        aria-label="More"
                        className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={180}>
                    <PopoverMenuItem onSelect={onDelete} icon={<Trash2Icon className="w-3.5 h-3.5" />} danger>
                        Delete
                    </PopoverMenuItem>
                </PopoverMenuContent>
            </PopoverMenu>
        </span>
    );
}

function SegmentChip({ segment }: { segment: string }) {
    return (
        <span className="shrink-0 max-w-[160px] truncate h-4 px-1.5 rounded bg-slate-100 text-slate-600 text-[10px] inline-flex items-center">
            {segment}
        </span>
    );
}

function TemplateDrawer({ template, days, onClose }: { template: WarmupTemplate; days: number; onClose: () => void }) {
    const stats = useWarmupTemplateStats(template.id, days);
    const full = useWarmupTemplate(template.id);
    const askDelete = useAskDelete(onClose);

    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);

    return (
        <>
            <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.18 }}
                className="fixed inset-0 z-40 bg-slate-900/40"
                onMouseDown={onClose}
            />
            <motion.aside
                role="dialog"
                aria-modal="true"
                aria-labelledby="warmup-template-title"
                initial={{ x: "100%" }}
                animate={{ x: 0 }}
                exit={{ x: "100%" }}
                transition={{ type: "spring", damping: 32, stiffness: 320 }}
                onMouseDown={(e) => e.stopPropagation()}
                className="fixed right-0 top-0 z-50 h-full w-full sm:w-[560px] bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex flex-col"
            >
                <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                    <div className="w-8 h-8 rounded-lg bg-sky-50 text-sky-700 flex items-center justify-center shrink-0">
                        <FileTextIcon className="w-4 h-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                        <div id="warmup-template-title" className="text-[13px] font-medium text-slate-900 truncate">
                            {template.name || template.subject}
                        </div>
                        <div className="text-[10.5px] text-slate-400">Added {fmtDate(template.created_at)}</div>
                    </div>
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Close"
                        className="w-7 h-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                <div className="flex-1 min-h-0 overflow-y-auto">
                    <dl className="px-5 py-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 border-b border-slate-200 text-[12.5px]">
                        <dt className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium pt-0.5">Subject</dt>
                        <dd className="text-slate-900 break-words">{template.subject}</dd>
                        <dt className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium pt-0.5">Segment</dt>
                        <dd className="text-slate-700">{template.segment || <span className="text-slate-400">Every mailbox</span>}</dd>
                        <dt className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium pt-0.5">Times used</dt>
                        <dd className="text-slate-700 tabular-nums">{template.usage_count.toLocaleString()}</dd>
                    </dl>

                    <div className="px-5 py-4 border-b border-slate-200">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-2">Body</div>
                        {full.data?.body ? (
                            <p className="whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[12.5px] leading-relaxed text-slate-700">
                                {full.data.body}
                            </p>
                        ) : (
                            <div className="h-16 rounded-md bg-slate-50 animate-pulse" />
                        )}
                    </div>

                    <div className="px-5 py-4 border-b border-slate-200">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-2">
                            Last {days} days at Google, Microsoft and Yahoo
                        </div>
                        <CopyPlacementSummary placement={stats.data?.total ?? template.placement ?? EMPTY_PLACEMENT} />
                    </div>

                    <div className="px-5 py-4 border-b border-slate-200">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-2">By provider</div>
                        {stats.isLoading ? (
                            <div className="space-y-2">
                                {[0, 1, 2].map((i) => (
                                    <div key={i} className="h-6 rounded bg-slate-50 animate-pulse" />
                                ))}
                            </div>
                        ) : stats.isError ? (
                            <p className="text-[12px] text-rose-600">{buildError(stats.error as unknown as AppError)}</p>
                        ) : (
                            <GroupBreakdown groups={stats.data?.groups} />
                        )}
                        <p className="mt-3 text-[11px] text-slate-400 leading-relaxed">{HEADLINE_NOTE}</p>
                    </div>

                    <div className="px-5 py-4">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-red-500 font-medium mb-2">Danger zone</div>
                        <div className="flex items-center gap-3">
                            <p className="flex-1 text-[11.5px] text-slate-500">Warmup stops starting new threads with this template.</p>
                            <button
                                type="button"
                                onClick={() => askDelete(template)}
                                className="h-7 px-2.5 rounded-md border border-red-200 text-[12px] font-medium text-red-600 hover:bg-red-50 inline-flex items-center gap-1.5 transition-colors shrink-0"
                            >
                                <Trash2Icon className="w-3.5 h-3.5" />
                                Delete
                            </button>
                        </div>
                    </div>
                </div>
            </motion.aside>
        </>
    );
}
