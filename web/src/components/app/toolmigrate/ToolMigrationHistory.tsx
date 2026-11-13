// Past and running migrations; a row opens it in the wizard at its preview or progress.

import React from "react";
import toast from "react-hot-toast/headless";
import { Loader2Icon, MoreHorizontalIcon } from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useCancelToolMigration, useDismissToolMigration, useToolMigrationProviders, useToolMigrations } from "@/lib/api/hooks/app/toolmigrate/useToolMigrations";
import type { ToolMigration, ToolMigrationScope } from "@/lib/api/models/app/toolmigrate/ToolMigration";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { countsSummary, followupCount, plural } from "./labels";
import { StatusPill, ToolGlyph } from "./parts";

export default function ToolMigrationHistory({
    scope,
    onOpen,
    empty,
}: {
    scope: ToolMigrationScope;
    onOpen: (id: string) => void;
    empty: React.ReactNode;
}) {
    const list = useToolMigrations(scope);
    const providers = useToolMigrationProviders(scope);
    const labels = new Map((providers.data?.data ?? []).map((p) => [p.id, p.label]));
    const rows = list.data?.data ?? [];

    if (list.isLoading) {
        return (
            <div className="h-24 flex items-center justify-center gap-2 text-[12px] text-slate-400">
                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                Loading migrations
            </div>
        );
    }
    if (list.isError) return <p className="px-5 py-4 text-[12px] text-red-600">{buildError(list.error as unknown as AppError)}</p>;
    if (rows.length === 0) return <>{empty}</>;

    return (
        <div>
            {rows.map((m) => (
                <HistoryRow key={m.id} scope={scope} migration={m} toolLabel={labels.get(m.provider) ?? m.label ?? m.provider} onOpen={() => onOpen(m.id)} />
            ))}
        </div>
    );
}

function HistoryRow({ scope, migration: m, toolLabel, onOpen }: { scope: ToolMigrationScope; migration: ToolMigration; toolLabel: string; onOpen: () => void }) {
    const confirm = useConfirm();
    const cancel = useCancelToolMigration(scope);
    const dismiss = useDismissToolMigration(scope);
    const summary = countsSummary(m);
    const todo = followupCount(m);
    const running = m.status === "running";
    const scanning = m.status === "scanning";
    const sources = (m.inventory ?? []).length;

    const detail =
        m.status === "ready"
            ? `Scanned ${plural(sources, "workspace")}, waiting for you to choose what to bring`
            : scanning
              ? "Reading the account"
              : summary || (m.status === "failed" ? m.error || "Stopped before anything came over" : "Nothing brought over yet");

    const askCancel = () =>
        confirm.show(`Cancel this migration? What came over so far stays, and the rest is left in ${toolLabel}.`, async () => {
            try {
                await cancel.mutateAsync(m.id);
            } catch (e) {
                toast.error(buildError(e as AppError));
            }
        });
    const askRemove = () =>
        confirm.show(
            "Remove this migration from the list? Everything it brought over stays. Its stored key is deleted.",
            async () => {
                try {
                    await dismiss.mutateAsync(m.id);
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );

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
            className="group min-h-12 px-4 md:px-5 py-2 flex items-center gap-3 border-b border-slate-200/60 hover:bg-slate-50/80 cursor-pointer outline-none focus-visible:bg-slate-50"
        >
            <ToolGlyph provider={m.provider} label={toolLabel} size={7} />
            <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 min-w-0">
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{toolLabel}</span>
                    <StatusPill status={m.status} />
                    {(running || scanning) && <Loader2Icon className="w-3 h-3 animate-spin text-sky-600 shrink-0" />}
                </div>
                <div className="text-[11.5px] text-slate-500 truncate">{detail}</div>
            </div>
            {todo > 0 && !running && (
                <span className="hidden sm:inline-flex h-5 px-1.5 rounded bg-amber-50 text-amber-700 text-[10.5px] font-medium items-center whitespace-nowrap">
                    {plural(todo, "follow-up")}
                </span>
            )}
            <span className="hidden md:block w-20 text-right text-[11.5px] text-slate-400 tabular-nums">{timeAgo(m.created_at)}</span>
            <span onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            aria-label={`Actions for the ${toolLabel} migration`}
                            className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus:opacity-100"
                        >
                            <MoreHorizontalIcon className="w-3.5 h-3.5" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={180}>
                        <PopoverMenuItem onSelect={onOpen}>{m.status === "ready" ? "Continue" : "Open"}</PopoverMenuItem>
                        {running && <PopoverMenuItem onSelect={askCancel}>Cancel migration</PopoverMenuItem>}
                        {!running && (
                            <PopoverMenuItem danger onSelect={askRemove}>
                                Remove from list
                            </PopoverMenuItem>
                        )}
                    </PopoverMenuContent>
                </PopoverMenu>
            </span>
        </div>
    );
}
