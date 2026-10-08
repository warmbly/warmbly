// Steady state after linking: every active mailbox, whether the cloud warms
// it, and the cloud's live view of the ones it does.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import { AlertTriangleIcon, Loader2Icon, PauseIcon, PlayIcon } from "lucide-react";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { CloudLinkMailboxRow, PoolLinkWarmupHealth } from "@/lib/api/models/app/cloudlink/CloudLink";
import {
    useCloudLinkMailboxLifecycle,
    useCloudLinkMailboxes,
    useEnrollCloudLinkMailbox,
    useUnenrollCloudLinkMailbox,
} from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { useConfirm } from "@/hooks/context/confirm";
import { TableSurface, Toggle } from "../_components/SectionShell";
import { providerLabel, providerSupported } from "./providers";
import { cloudWarmupPaused } from "@/lib/cloudWarmup";

export default function MailboxTable() {
    const rows = useCloudLinkMailboxes();
    const enroll = useEnrollCloudLinkMailbox();
    const unenroll = useUnenrollCloudLinkMailbox();
    const lifecycle = useCloudLinkMailboxLifecycle();
    const confirm = useConfirm();
    const [busy, setBusy] = React.useState<string | null>(null);

    const run = async (id: string, fn: () => Promise<unknown>, ok: string) => {
        setBusy(id);
        try {
            await fn();
            toast.success(ok);
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setBusy(null);
        }
    };

    const flip = (row: CloudLinkMailboxRow) => {
        if (row.enrolled) {
            confirm.show(
                row.managed
                    ? `Remove ${row.email} from this instance? It stays in your Warmbly Cloud workspace, where its sign-in lives.`
                    : `Stop warming ${row.email} in the Warmbly pool? The cloud deletes its credential right away.`,
                async () => {
                    await run(row.id, () => unenroll.mutateAsync(row.id), row.managed ? `${row.email} removed from this instance` : `${row.email} removed from the pool`);
                },
            );
            return;
        }
        void run(row.id, () => enroll.mutateAsync(row.id), `${row.email} is now warming in the pool`);
    };

    if (rows.isLoading) {
        return (
            <div className="py-8 flex justify-center text-slate-400">
                <Loader2Icon className="w-4 h-4 animate-spin" />
            </div>
        );
    }
    const list = rows.data ?? [];
    if (list.length === 0) {
        return <p className="text-[12.5px] text-slate-500">No active mailboxes. Connect one under Mailboxes to enroll it.</p>;
    }

    return (
        <TableSurface>
            <table className="w-full text-[12.5px]">
                <thead>
                    <tr className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium border-b border-slate-200/70">
                        <th className="text-left font-medium px-3 h-8">Mailbox</th>
                        <th className="text-left font-medium px-3 h-8 hidden md:table-cell">Health</th>
                        <th className="text-right font-medium px-3 h-8 hidden md:table-cell">Today</th>
                        <th className="text-right font-medium px-3 h-8 hidden lg:table-cell">7 days</th>
                        <th className="text-right font-medium px-3 h-8">In pool</th>
                    </tr>
                </thead>
                <tbody className="divide-y divide-slate-200/70">
                    <AnimatePresence initial={false}>
                        {list.map((row) => {
                            const supported = providerSupported(row.provider) || row.managed;
                            const cloud = row.cloud;
                            const paused = cloudWarmupPaused(cloud);
                            return (
                                <motion.tr key={row.id} layout initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="bg-white">
                                    <td className="px-3 py-2.5">
                                        <div className="min-w-0">
                                            <p className="text-slate-900 truncate">{row.email}</p>
                                            <p className="text-[11px] text-slate-400 truncate">
                                                {providerLabel(row.provider)}
                                                {row.managed && " · signed in through Warmbly Cloud"}
                                                {!supported && " · signed in with this instance's own OAuth app; add it again through Warmbly Cloud to warm it"}
                                                {row.enrolled && !cloud && " · waiting for the cloud"}
                                                {cloud?.errors && cloud.errors.length > 0 && (
                                                    <span className="inline-flex items-center gap-1 text-amber-700 ml-1">
                                                        <AlertTriangleIcon className="w-3 h-3" />
                                                        {cloud.errors[0].title}
                                                    </span>
                                                )}
                                            </p>
                                        </div>
                                    </td>
                                    <td className="px-3 py-2.5 hidden md:table-cell">
                                        {row.enrolled ? <HealthPill health={cloud?.health ?? null} paused={paused} /> : <span className="text-slate-300">–</span>}
                                    </td>
                                    <td className="px-3 py-2.5 text-right tabular-nums hidden md:table-cell">
                                        {row.enrolled && cloud ? (
                                            <span className="text-slate-700">
                                                {cloud.sent_today}
                                                <span className="text-slate-400"> / {cloud.warmup?.target_volume ?? cloud.settings.base}</span>
                                            </span>
                                        ) : (
                                            <span className="text-slate-300">–</span>
                                        )}
                                    </td>
                                    <td className="px-3 py-2.5 text-right tabular-nums hidden lg:table-cell">
                                        {row.enrolled && cloud ? (
                                            <span className="text-slate-700" title={`${cloud.replied_7d} replies, ${cloud.spam_placed_7d} landed in spam`}>
                                                {cloud.sent_7d} sent
                                                {cloud.spam_placed_7d > 0 && <span className="text-amber-700"> · {cloud.spam_placed_7d} spam</span>}
                                            </span>
                                        ) : (
                                            <span className="text-slate-300">–</span>
                                        )}
                                    </td>
                                    <td className="px-3 py-2.5">
                                        <div className="flex items-center justify-end gap-2">
                                            {row.enrolled && cloud && (
                                                <button
                                                    type="button"
                                                    title={paused ? "Resume warmup" : "Pause warmup"}
                                                    onClick={() =>
                                                        void run(
                                                            row.id,
                                                            () => lifecycle.mutateAsync({ id: row.id, action: paused ? "resume" : "pause" }),
                                                            paused ? "Warmup resumed" : "Warmup paused",
                                                        )
                                                    }
                                                    className="size-6 rounded-md inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                                                >
                                                    {paused ? <PlayIcon className="w-3.5 h-3.5" /> : <PauseIcon className="w-3.5 h-3.5" />}
                                                </button>
                                            )}
                                            {busy === row.id ? (
                                                <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                                            ) : (
                                                <Toggle on={row.enrolled} disabled={!supported} onChange={() => flip(row)} />
                                            )}
                                        </div>
                                    </td>
                                </motion.tr>
                            );
                        })}
                    </AnimatePresence>
                </tbody>
            </table>
        </TableSurface>
    );
}

function HealthPill({ health, paused }: { health: PoolLinkWarmupHealth | null; paused: boolean }) {
    if (paused) return <Pill tone="muted">Paused</Pill>;
    if (!health) return <Pill tone="muted">Starting</Pill>;
    switch (health.state) {
        case "healthy":
            return <Pill tone="ok">Healthy</Pill>;
        case "watch":
            return <Pill tone="warn">Watch</Pill>;
        case "throttled":
            return <Pill tone="warn">Throttled</Pill>;
        default:
            return (
                <Pill tone="bad" title={health.reason}>
                    {health.state === "blocked" ? "Blocked" : "Quarantined"}
                </Pill>
            );
    }
}

function Pill({ tone, children, title }: { tone: "ok" | "warn" | "bad" | "muted"; children: React.ReactNode; title?: string }) {
    const cls =
        tone === "ok"
            ? "bg-emerald-50 text-emerald-700"
            : tone === "warn"
              ? "bg-amber-50 text-amber-700"
              : tone === "bad"
                ? "bg-rose-50 text-rose-700"
                : "bg-slate-100 text-slate-500";
    return (
        <span title={title} className={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium ${cls}`}>
            {children}
        </span>
    );
}
