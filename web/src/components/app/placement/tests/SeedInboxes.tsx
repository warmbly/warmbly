// The workspace's own seed panel: any connected mailbox can be marked as a
// seed inbox. Tests on this panel are never counted toward the monthly
// allowance, and a seed never warms up or sends campaign mail.

import React from "react";
import { InboxIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast";
import { Link } from "react-router-dom";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { EmptyBlock, SectionBar } from "@/components/layout/Page";
import { SearchInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import { usePlacementSeeds, useSetPlacementSeed } from "@/lib/api/hooks/app/placement/usePlacement";
import type { PlacementWorkspaceSeed } from "@/lib/api/models/app/placement/Placement";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

const STATUS_LABEL: Record<string, string> = {
    active: "Connected",
    inactive: "Off",
    revoked: "Needs reconnecting",
};

export default function SeedInboxes() {
    const seeds = usePlacementSeeds();
    const canManage = usePermission("MANAGE_EMAILS");
    const [q, setQ] = React.useState("");
    const rows = seeds.data ?? [];
    const needle = q.trim().toLowerCase();
    const shown = needle ? rows.filter((r) => r.email.toLowerCase().includes(needle) || r.label.toLowerCase().includes(needle)) : rows;
    // Seeds first, so the panel reads at a glance.
    const sorted = [...shown].sort((a, b) => Number(b.seed) - Number(a.seed) || a.email.localeCompare(b.email));
    const seedCount = rows.filter((r) => r.seed).length;

    return (
        <section>
            <div className="px-5 py-4 border-b border-slate-200/60 max-w-3xl">
                <p className="text-[12.5px] text-slate-700 leading-relaxed">
                    A seed inbox receives test copies so you can see where they land. Tests on your own seeds are never
                    counted toward your monthly tests.
                </p>
                <p className="mt-1.5 text-[11.5px] text-slate-500 leading-relaxed">
                    A seed never warms up and never sends campaign mail, so use a test mailbox, not one you send from.
                    It should be on a different domain than your senders: a personal Gmail, Outlook.com or Yahoo address
                    works best. Seeds on a sender&apos;s own domain are skipped, because mail inside one domain is not
                    filtered like mail from outside.
                </p>
            </div>
            <SectionBar label="Mailboxes" count={seeds.data ? `${seedCount} seed${seedCount === 1 ? "" : "s"}` : undefined}>
                <div className="w-full sm:w-56">
                    <SearchInput value={q} onChange={setQ} placeholder="Search mailboxes…" />
                </div>
            </SectionBar>
            {seeds.isLoading ? (
                <div className="divide-y divide-slate-200/60">
                    {Array.from({ length: 4 }).map((_, i) => (
                        <div key={i} className="h-12 px-5 flex items-center">
                            <div className="h-3 w-56 rounded bg-slate-100 animate-pulse" />
                        </div>
                    ))}
                </div>
            ) : seeds.isError ? (
                <EmptyBlock title="Seed inboxes could not be loaded" body={buildError(seeds.error as unknown as AppError)} />
            ) : rows.length === 0 ? (
                <EmptyBlock
                    title="No mailboxes yet"
                    body="Connect a test mailbox, then mark it as a seed here."
                    cta={
                        <Link
                            to="/app/emails"
                            className="h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium bg-sky-600 hover:bg-sky-700 text-white transition-colors"
                        >
                            Connect a mailbox
                        </Link>
                    }
                />
            ) : sorted.length === 0 ? (
                <EmptyBlock title="No mailbox matches that" />
            ) : (
                <div className="divide-y divide-slate-200/60">
                    {sorted.map((r) => (
                        <SeedRow key={r.email_account_id} row={r} canManage={canManage} />
                    ))}
                </div>
            )}
        </section>
    );
}

function SeedRow({ row, canManage }: { row: PlacementWorkspaceSeed; canManage: boolean }) {
    const set = useSetPlacementSeed();
    const confirm = useConfirm();

    const apply = async (seed: boolean) => {
        try {
            await set.mutateAsync({ emailAccountId: row.email_account_id, seed });
            toast.success(seed ? `${row.email} is now a seed inbox.` : `${row.email} is no longer a seed inbox.`);
        } catch (e) {
            const err = e as AppError;
            toast.error(
                err?.code === "placement_seed_limit"
                    ? "A workspace can have up to 50 seed inboxes."
                    : err?.code === "placement_seed_unavailable"
                      ? err.message || "This mailbox cannot be changed right now."
                      : buildError(err),
            );
        }
    };

    // Turning a seed on stops its warmup, so it is confirmed; turning it off is harmless.
    const toggle = (seed: boolean) => {
        if (!seed) {
            void apply(false);
            return;
        }
        confirm.show(
            `Make ${row.email} a seed inbox? Its warmup turns off and it stops sending campaign mail while it is a seed.`,
            () => apply(true),
        );
    };

    const blocked = !!row.blocker;
    const disabled = !canManage || blocked || set.isPending;

    return (
        <div className="min-h-12 px-5 py-2 flex items-center gap-3">
            <InboxIcon className={cn("w-3.5 h-3.5 shrink-0", row.seed ? "text-sky-600" : "text-slate-300")} />
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                    <span className="text-[12.5px] font-medium text-slate-900 truncate max-w-full">{row.email}</span>
                    {row.label && <span className="text-[11px] text-slate-400">{row.label}</span>}
                    {row.status !== "active" && (
                        <span className="h-4 px-1.5 rounded bg-amber-50 text-amber-700 text-[10px] inline-flex items-center">
                            {STATUS_LABEL[row.status] ?? row.status}
                        </span>
                    )}
                </div>
                {row.blocker && <p className="mt-0.5 text-[11px] text-slate-500 leading-snug">{row.blocker}</p>}
            </div>
            <span className="hidden sm:inline text-[11px] text-slate-400 shrink-0">{row.seed ? "Seed" : ""}</span>
            {set.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400 shrink-0" />}
            <span title={!canManage ? "You need the Manage mailboxes permission" : blocked ? row.blocker : undefined}>
                <Toggle
                    value={row.seed}
                    onChange={toggle}
                    disabled={disabled}
                    ariaLabel={row.seed ? `Stop using ${row.email} as a seed` : `Use ${row.email} as a seed`}
                />
            </span>
        </div>
    );
}
