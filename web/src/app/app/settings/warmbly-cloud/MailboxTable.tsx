// Steady state after linking: every active mailbox, whether the cloud warms
// it, and the cloud's live view of the ones it does.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import { AlertTriangleIcon, CloudIcon, Loader2Icon, PauseIcon, PlayIcon, ServerIcon } from "lucide-react";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { CloudLinkExecutor, CloudLinkMailboxRow, PoolLinkSelfExecution, PoolLinkWarmupHealth } from "@/lib/api/models/app/cloudlink/CloudLink";
import {
    useCloudLinkMailboxLifecycle,
    useCloudLinkMailboxes,
    useEnrollCloudLinkMailbox,
    useUnenrollCloudLinkMailbox,
} from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { useConfirm } from "@/hooks/context/confirm";
import { cloudMailboxPickerOpened } from "@/lib/cloudMailboxConnect";
import { TableSurface, Toggle } from "../_components/SectionShell";
import { providerLabel, providerSupported } from "./providers";
import { resolveMailboxWarmup, warmupActivityLabel, warmupUnknown } from "@/lib/mailboxWarmup";
import { cloudStandingUnavailable } from "@/lib/mailboxStatus";

/** Whether Cloud lets this instance keep mailbox credentials: offered, missing approval, or not supported. */
function selfExecutionState(se: PoolLinkSelfExecution | null | undefined): "offered" | "not_granted" | "unsupported" {
    if (!se || se.protocol < 1) return "unsupported";
    if (!se.granted || (se.expires_at && new Date(se.expires_at).getTime() <= Date.now())) return "not_granted";
    return "offered";
}

/** A mailbox Cloud signs in to itself has no credential here to keep. */
function canKeepCredential(row: CloudLinkMailboxRow): boolean {
    return !row.managed && !row.independent_smtp && !row.cloud?.managed;
}

export default function MailboxTable({ selfExecution }: { selfExecution?: PoolLinkSelfExecution | null }) {
    const rows = useCloudLinkMailboxes();
    const selfExec = selfExecutionState(selfExecution);
    const [choice, setChoice] = React.useState<Record<string, CloudLinkExecutor>>({});
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
            if (!cloudMailboxPickerOpened(e)) toast.error(buildError(e as AppError));
        } finally {
            setBusy(null);
        }
    };

    const flip = (row: CloudLinkMailboxRow) => {
        if (row.enrolled) {
            confirm.show(
                row.managed
                    ? `Remove ${row.email} from this instance and stop its Cloud warmup sending and receiving? Its mailbox, sign-in and history stay in your Warmbly Cloud workspace. You can restart warmup there.`
                    : row.independent_smtp || row.cloud?.managed
                    ? `Disconnect Cloud warmup for ${row.email}? Its Cloud mailbox, credentials and warmup history stay on Cloud. Campaigns here keep their existing app-password connection. Cloud warmup sending and receiving stop until you restart them there.`
                    : row.executor === "instance"
                    ? `Stop warming ${row.email} in the Warmbly pool? Its credential never left this server.`
                    : `Stop warming ${row.email} in the Warmbly pool? The cloud deletes its credential right away.`,
                async () => {
                    await run(row.id, () => unenroll.mutateAsync(row.id), row.managed ? `${row.email} removed from this instance` : `${row.email} removed from the pool`);
                },
            );
            return;
        }
        const executor = chosenExecutor(row);
        void run(row.id, () => enroll.mutateAsync(executor === "instance" ? { id: row.id, executor } : row.id), `${row.email} is now warming in the pool`);
    };

    const chosenExecutor = (row: CloudLinkMailboxRow): CloudLinkExecutor => {
        if (row.enrolled) return row.executor ?? "cloud";
        if (selfExec !== "offered" || !canKeepCredential(row)) return "cloud";
        return choice[row.id] ?? "instance";
    };

    const pickExecutor = (row: CloudLinkMailboxRow, executor: CloudLinkExecutor) => {
        if (!row.enrolled) {
            setChoice((c) => ({ ...c, [row.id]: executor }));
            return;
        }
        if ((row.executor ?? "cloud") === executor) return;
        confirm.show(
            executor === "instance"
                ? `Keep ${row.email}'s credential on this server? Warmbly Cloud deletes its copy, and this server's workers send and sync its warmup from now on.`
                : `Hand ${row.email}'s credential to Warmbly Cloud? Cloud keeps an encrypted copy and runs its warmup itself.`,
            async () => {
                await run(
                    row.id,
                    () => enroll.mutateAsync({ id: row.id, executor }),
                    executor === "instance" ? `${row.email}'s credential stays on this server` : `Warmbly Cloud now runs ${row.email}'s warmup`,
                );
            },
        );
    };

    const anyHere = (rows.data ?? []).some((r) => r.enrolled && r.executor === "instance");
    const movable = (rows.data ?? []).filter((r) => r.enrolled && (r.executor ?? "cloud") === "cloud" && canKeepCredential(r) && providerSupported(r.provider));

    const keepAllHere = () =>
        confirm.show(
            `Keep the credentials of ${movable.length === 1 ? movable[0].email : `${movable.length} enrolled mailboxes`} on this server? Warmbly Cloud deletes its copies, and this server's workers send and sync their warmup from now on.`,
            async () => {
                let moved = 0;
                for (const row of movable) {
                    setBusy(row.id);
                    try {
                        await enroll.mutateAsync({ id: row.id, executor: "instance" });
                        moved++;
                    } catch (e) {
                        toast.error(`${row.email}: ${buildError(e as AppError)}`);
                    }
                }
                setBusy(null);
                if (moved > 0) toast.success(moved === 1 ? "1 credential now stays on this server" : `${moved} credentials now stay on this server`);
            },
        );

    if (rows.isLoading) {
        return (
            <div className="py-8 flex justify-center text-slate-400">
                <Loader2Icon className="w-4 h-4 animate-spin" />
            </div>
        );
    }
    const list = rows.data ?? [];
    if (rows.isError) return <p className="text-[12.5px] text-slate-500">Cloud warmup status unavailable. Refresh to try again.</p>;
    if (list.length === 0) {
        return <p className="text-[12.5px] text-slate-500">No active mailboxes. Connect one under Mailboxes to enroll it.</p>;
    }

    return (
        <>
            {selfExec === "offered" && (
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                    <p className="text-[12px] text-slate-500">
                        A mailbox can keep its credential on this server: Warmbly Cloud plans its warmup and this server's own workers send and sync it. Choose per mailbox below.
                    </p>
                    {movable.length > 1 && (
                        <button
                            type="button"
                            onClick={keepAllHere}
                            disabled={busy !== null}
                            className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 disabled:opacity-50"
                        >
                            <ServerIcon className="w-3.5 h-3.5" />
                            Keep all on this server
                        </button>
                    )}
                </div>
            )}
            {selfExec !== "offered" && anyHere && (
                <p className="text-[12px] text-amber-700 mb-2 inline-flex items-center gap-1.5">
                    <AlertTriangleIcon className="w-3.5 h-3.5 shrink-0" />
                    {selfExec === "unsupported"
                        ? "The connected Warmbly Cloud no longer offers keeping credentials on this server, so mailboxes set that way are not warming."
                        : "Keeping credentials on this server is no longer approved for your Cloud workspace, so mailboxes set that way are not warming. Ask Warmbly support, or hand the credential to Cloud."}
                </p>
            )}
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
                                const warmup = resolveMailboxWarmup(row, undefined, { connected: true, rowFor: () => row });
                                const unknown = warmupUnknown(warmup);
                                const cloudActivity = warmup.cloudActivity ?? warmup.activity;
                                const paused = cloudActivity === "paused" || cloudActivity === "receive_only";
                                const resume = paused || cloudActivity === "off";
                                return (
                                    <motion.tr key={row.id} layout initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="bg-white">
                                        <td className="px-3 py-2.5">
                                            <div className="min-w-0">
                                                <p className="text-slate-900 truncate">{row.email}</p>
                                                <p className="text-[11px] text-slate-400 truncate">
                                                    {providerLabel(row.provider)}
                                                    {row.managed && " · signed in through Warmbly Cloud"}
                                                    {!supported && " · signed in with this instance's own OAuth app; add it again through Warmbly Cloud to warm it"}
                                                    {row.enrolled && row.executor === "instance" && " · credential stays on this server"}
                                                    {row.enrolled && ` · ${warmupActivityLabel[warmup.activity]}`}
                                                    {row.enrolled && row.executor === "instance" && (row.commands_failed ?? 0) > 0 && (
                                                        <span className="text-amber-700" title="Commands from Warmbly Cloud this server failed, refused or let expire in the last day">
                                                            {` · ${row.commands_failed} not run today`}
                                                        </span>
                                                    )}
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
                                            {row.enrolled ? unknown ? <Pill tone="muted">Unavailable</Pill> : <HealthPill health={cloud?.health ?? null} paused={paused} /> : <span className="text-slate-300">–</span>}
                                        </td>
                                        <td className="px-3 py-2.5 text-right tabular-nums hidden md:table-cell">
                                            {row.enrolled && !unknown && cloud ? (
                                                <span className="text-slate-700">
                                                    {cloud.sent_today}
                                                    <span className="text-slate-400"> / {cloud.warmup?.target_volume ?? cloud.settings.base}</span>
                                                </span>
                                            ) : (
                                                <span className="text-slate-300">–</span>
                                            )}
                                        </td>
                                        <td className="px-3 py-2.5 text-right tabular-nums hidden lg:table-cell">
                                            {row.enrolled && !unknown && cloud ? (
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
                                                {supported && canKeepCredential(row) && (selfExec === "offered" || (row.enrolled && row.executor === "instance")) && (
                                                    <ExecutorSwitch
                                                        value={chosenExecutor(row)}
                                                        disabled={busy === row.id || (selfExec !== "offered" && chosenExecutor(row) === "cloud")}
                                                        allowInstance={selfExec === "offered"}
                                                        onChange={(v) => pickExecutor(row, v)}
                                                    />
                                                )}
                                                {row.enrolled && !unknown && cloud && cloudActivity !== "inactive" && (
                                                    <button
                                                        type="button"
                                                        title={resume ? "Resume warmup" : "Pause warmup"}
                                                        onClick={() =>
                                                            void run(
                                                                row.id,
                                                                () => lifecycle.mutateAsync({ id: row.id, action: resume ? "resume" : "pause" }),
                                                                resume ? "Warmup resumed" : "Warmup paused",
                                                            )
                                                        }
                                                        className="size-6 rounded-md inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                                                    >
                                                        {resume ? <PlayIcon className="w-3.5 h-3.5" /> : <PauseIcon className="w-3.5 h-3.5" />}
                                                    </button>
                                                )}
                                                {busy === row.id ? (
                                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                                                ) : (
                                                    <Toggle on={row.enrolled} disabled={!supported || row.enrollment_state === "pending_remove" || row.enrollment_state === "pending_enroll"} onChange={() => flip(row)} />
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
        </>
    );
}

function ExecutorSwitch({
    value,
    disabled,
    allowInstance,
    onChange,
}: {
    value: CloudLinkExecutor;
    disabled?: boolean;
    allowInstance: boolean;
    onChange: (v: CloudLinkExecutor) => void;
}) {
    const option = (v: CloudLinkExecutor, label: string, title: string, Icon: typeof CloudIcon, enabled: boolean) => (
        <button
            type="button"
            title={title}
            aria-pressed={value === v}
            disabled={disabled || !enabled}
            onClick={(e) => {
                e.stopPropagation();
                onChange(v);
            }}
            className={`h-6 px-1.5 inline-flex items-center gap-1 text-[11px] rounded transition-colors disabled:opacity-50 ${
                value === v ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-800"
            }`}
        >
            <Icon className="w-3 h-3" />
            <span className="hidden sm:inline">{label}</span>
        </button>
    );
    return (
        <div className="inline-flex items-center gap-0.5 p-0.5 rounded-md bg-slate-100" role="group" aria-label="Where the credential lives">
            {option("instance", "This server", "The credential stays on this server; its workers send and sync the warmup", ServerIcon, allowInstance)}
            {option("cloud", "Cloud", "Warmbly Cloud keeps an encrypted copy of the credential and runs the warmup", CloudIcon, true)}
        </div>
    );
}

function HealthPill({ health, paused }: { health: PoolLinkWarmupHealth | null; paused: boolean }) {
    if (paused) return <Pill tone="muted">Paused</Pill>;
    if (!health) return <Pill tone="muted">Starting</Pill>;
    if (cloudStandingUnavailable(health))
        return (
            <Pill tone="warn" title="Warmbly Cloud has not reported a current status. Campaign sends wait for one; this is not a block.">
                Unavailable
            </Pill>
        );
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
