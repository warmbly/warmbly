// SigninMigrationDialog: every mailbox still on per-mailbox Google sign-in,
// by domain, with the move that fits it. A domain with an admin grant moves
// in one click (an import, shown with the import result screen); a Workspace
// domain without one is set up first; a personal address switches to an app
// password. Every move keeps the mailbox, its history, campaigns and warmup.
import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import { useIsMutating } from "@tanstack/react-query";
import { ArrowLeftIcon, ArrowRightIcon, Building2Icon, CheckCircle2Icon, KeyRoundIcon, Loader2Icon, XIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { useConfirm } from "@/hooks/context/confirm";
import {
    MoveError,
    SIGNIN_MIGRATION_MUTATION,
    useGrantConfig,
    useMoveToGrant,
    useSigninMigration,
} from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { useMailboxSourceBusy } from "@/lib/api/hooks/app/emails/mailboxSourceBusy";
import type { MigrationMailbox, SigninMigration } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { plural } from "../import/importFields";
import RunStep from "../import/RunStep";
import AppPasswordSwitchForm from "./AppPasswordSwitchForm";
import { migrationRoute, type MigrationRoute } from "./migrationRoute";
import { SkeletonCards } from "@/components/app/emails/import/Discovering";

export default function SigninMigrationDialog({
    open,
    focusDomain,
    onClose,
    onSetUpDomain,
}: {
    open: boolean;
    /** Scrolled to and marked, e.g. from one mailbox's row. */
    focusDomain?: string | null;
    onClose: () => void;
    /** Opens the whole-domain setup; the dialog closes first. */
    onSetUpDomain: (domain: string) => void;
}) {
    const migration = useSigninMigration(open);
    const googleGrants = useGrantConfig(open).data?.google_enabled === true;
    const confirm = useConfirm();
    const [run, setRun] = React.useState<{ importId: string; domain: string } | null>(null);
    const [dirty, setDirty] = React.useState<Set<string>>(() => new Set());

    React.useEffect(() => {
        if (!open) {
            setRun(null);
            setDirty(new Set());
        }
    }, [open]);

    const migrating = useIsMutating({ mutationKey: SIGNIN_MIGRATION_MUTATION }) > 0;
    const busy = (useMailboxSourceBusy() || migrating) && open;

    const requestClose = React.useCallback(() => {
        if (busy) return;
        if (dirty.size > 0) {
            confirm.show("Discard the app password you typed? The mailbox stays on Google sign-in.", async () => onClose());
            return;
        }
        onClose();
    }, [busy, dirty, confirm, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    const setRowDirty = React.useCallback((id: string, d: boolean) => {
        setDirty((prev) => {
            if (prev.has(id) === d) return prev;
            const next = new Set(prev);
            if (d) next.add(id);
            else next.delete(id);
            return next;
        });
    }, []);

    const groups = React.useMemo(() => {
        const all = migration.data?.data ?? [];
        if (!focusDomain) return all;
        return [...all].sort((a, b) => Number(b.domain === focusDomain) - Number(a.domain === focusDomain));
    }, [migration.data, focusDomain]);
    const total = migration.data?.total ?? 0;

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Move off Google sign-in"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className={cn(
                            "w-full rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh] transition-[max-width] duration-200",
                            run ? "max-w-[760px]" : "max-w-[620px]",
                        )}
                    >
                        <div className="h-12 px-3 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            {run ? (
                                <button
                                    type="button"
                                    onClick={() => setRun(null)}
                                    aria-label="Back to the list"
                                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                >
                                    <ArrowLeftIcon className="w-3.5 h-3.5" />
                                </button>
                            ) : (
                                <div className="size-5 ml-1 rounded bg-slate-100 flex items-center justify-center">
                                    <ProviderLogo id="google" size="xs" framed={false} />
                                </div>
                            )}
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Mailbox</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12px] text-slate-600 truncate">
                                {run ? `Moving ${run.domain} onto the admin grant` : "Move off Google sign-in"}
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                disabled={busy}
                                aria-label="Close"
                                title={busy ? "Wait for this to finish" : undefined}
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            <AnimatePresence mode="wait" initial={false}>
                                <motion.div
                                    key={run ? "run" : "list"}
                                    initial={{ opacity: 0, x: run ? 16 : -16 }}
                                    animate={{ opacity: 1, x: 0 }}
                                    exit={{ opacity: 0, x: run ? -16 : 16 }}
                                    transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
                                >
                                    {run ? (
                                        <RunStep
                                            key={run.importId}
                                            importId={run.importId}
                                            onDone={onClose}
                                            onImportAnother={() => setRun(null)}
                                            anotherLabel="Move more mailboxes"
                                        />
                                    ) : migration.isLoading ? (
                                        <div className="p-4" role="status" aria-label="Loading the mailboxes to move">
                                            <SkeletonCards count={2} />
                                        </div>
                                    ) : total === 0 ? (
                                        <div className="px-6 py-10 flex flex-col items-center text-center gap-2">
                                            <CheckCircle2Icon className="w-6 h-6 text-emerald-500" />
                                            <p className="text-[13px] font-medium text-slate-900">Every mailbox is off Google sign-in</p>
                                            <p className="text-[12px] text-slate-500 max-w-sm">Nothing left to move. They keep working as they are.</p>
                                            <button
                                                type="button"
                                                onClick={onClose}
                                                className="mt-2 h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                                            >
                                                Close
                                            </button>
                                        </div>
                                    ) : (
                                        <div className="p-4 space-y-3">
                                            <p className="text-[12px] text-slate-600 leading-relaxed">
                                                Switching from Google sign-in is optional. Moving keeps each mailbox, with its history,
                                                campaigns and warmup.
                                            </p>
                                            {groups.map((g) => (
                                                <GroupCard
                                                    key={g.domain}
                                                    group={g}
                                                    route={migrationRoute(g, googleGrants)}
                                                    focused={g.domain === focusDomain}
                                                    onMoved={(importId) => setRun({ importId, domain: g.domain })}
                                                    onSetUp={() => onSetUpDomain(g.domain)}
                                                    setRowDirty={setRowDirty}
                                                />
                                            ))}
                                        </div>
                                    )}
                                </motion.div>
                            </AnimatePresence>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}

function GroupCard({
    group: g,
    route,
    focused,
    onMoved,
    onSetUp,
    setRowDirty,
}: {
    group: SigninMigration;
    route: MigrationRoute;
    focused: boolean;
    onMoved: (importId: string) => void;
    onSetUp: () => void;
    setRowDirty: (id: string, dirty: boolean) => void;
}) {
    const ref = React.useRef<HTMLDivElement>(null);
    const move = useMoveToGrant();
    const n = g.mailboxes.length;

    React.useEffect(() => {
        if (focused) ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }, [focused]);

    async function moveAll() {
        if (!g.grant_id || move.isPending) return;
        try {
            const out = await move.mutateAsync({ grantId: g.grant_id, emails: g.mailboxes.map((m) => m.email) });
            toast.success(`Moving ${plural(out.moved, "mailbox", "mailboxes")} onto the admin grant`);
            if (out.missing > 0) {
                toast(`${plural(out.missing, "address is", "addresses are")} not an active account in the directory. Switch ${out.missing === 1 ? "it" : "them"} to an app password instead.`);
            }
            onMoved(out.job.id);
        } catch (e) {
            toast.error(e instanceof MoveError ? e.message : buildError(e as AppError));
        }
    }

    const sub =
        route === "grant"
            ? "This domain has an admin grant. Moving signs each mailbox in through it."
            : route === "setup"
              ? "Set up the whole domain once, then move these mailboxes onto it."
              : g.kind === "personal"
                ? "A personal address moves to an app password, one mailbox at a time."
                : "Admin grants are not set up on this instance, so each mailbox moves to an app password.";

    return (
        <div
            ref={ref}
            className={cn(
                "rounded-md border overflow-hidden transition-colors",
                focused ? "border-sky-300 ring-2 ring-sky-100" : "border-slate-200",
            )}
        >
            <div className="px-3 py-2.5 flex items-start gap-2.5 bg-slate-50/60 border-b border-slate-100">
                <ProviderLogo id={g.kind === "personal" ? "gmail" : "google"} size="lg" />
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 min-w-0">
                        <span className="text-[12.5px] font-medium text-slate-900 truncate">{g.domain}</span>
                        <span className="text-[11px] text-slate-500 tabular-nums shrink-0">{plural(n, "mailbox", "mailboxes")}</span>
                    </div>
                    <p className="text-[11.5px] text-slate-500 leading-snug">{sub}</p>
                </div>
                {route === "grant" && (
                    <button
                        type="button"
                        onClick={() => void moveAll()}
                        aria-disabled={move.isPending}
                        className={cn(
                            "shrink-0 self-center h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                            move.isPending && "opacity-60",
                        )}
                    >
                        {move.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ArrowRightIcon className="w-3 h-3" />}
                        Move {n} to the admin grant
                    </button>
                )}
                {route === "setup" && (
                    <button
                        type="button"
                        onClick={onSetUp}
                        className="shrink-0 self-center h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <Building2Icon className="w-3 h-3" />
                        Set up the whole domain
                    </button>
                )}
            </div>
            <div className="divide-y divide-slate-100">
                {g.mailboxes.map((m) => (
                    <MailboxLine key={m.id} mailbox={m} appPassword={route === "app_password"} setRowDirty={setRowDirty} />
                ))}
            </div>
        </div>
    );
}

function MailboxLine({
    mailbox: m,
    appPassword,
    setRowDirty,
}: {
    mailbox: MigrationMailbox;
    appPassword: boolean;
    setRowDirty: (id: string, dirty: boolean) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const onDirty = React.useCallback((d: boolean) => setRowDirty(m.id, d), [m.id, setRowDirty]);
    return (
        <div className="px-3 py-2">
            <div className="flex items-center gap-2 min-w-0">
                <div className="min-w-0 flex-1">
                    <div className="text-[12px] text-slate-800 truncate">{m.email}</div>
                    {m.name && <div className="text-[11px] text-slate-500 truncate">{m.name}</div>}
                </div>
                {m.status && m.status !== "active" && (
                    <span className="shrink-0 h-[18px] px-1.5 rounded bg-slate-100 text-slate-600 text-[10.5px] font-medium inline-flex items-center capitalize">
                        {m.status.replace(/_/g, " ")}
                    </span>
                )}
                {appPassword && (
                    <button
                        type="button"
                        onClick={() => setOpen((v) => !v)}
                        aria-expanded={open}
                        className={cn(
                            "shrink-0 h-6 px-2 rounded-md border text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors",
                            open ? "border-sky-200 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50",
                        )}
                    >
                        <KeyRoundIcon className="w-3 h-3" />
                        <span className="hidden sm:inline">Switch to an app password</span>
                        <span className="sm:hidden">App password</span>
                    </button>
                )}
            </div>
            <AnimatePresence initial={false}>
                {appPassword && open && (
                    <motion.div
                        key="form"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.18 }}
                        className="overflow-hidden"
                    >
                        <div className="pt-2">
                            <AppPasswordSwitchForm mailbox={m} onDirtyChange={onDirty} onDone={() => setOpen(false)} />
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
