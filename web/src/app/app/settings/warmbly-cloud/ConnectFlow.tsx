// The three-step link flow: link this instance to a Warmbly Cloud workspace,
// pick the mailboxes the cloud should warm, done. Steps slide directionally
// like NewCampaignDialog; a step cannot be skipped ahead of.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { ArrowRightIcon, CheckIcon, CloudIcon, FlameIcon, InboxIcon, Loader2Icon, SparklesIcon } from "lucide-react";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { CloudLinkMailboxRow, CloudLinkStatus } from "@/lib/api/models/app/cloudlink/CloudLink";
import { useCloudLinkMailboxes, useEnrollCloudLinkMailbox, useUnenrollCloudLinkMailbox } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import CloudLinkCard from "@/components/app/cloud/CloudLinkCard";
import { Toggle } from "../_components/SectionShell";
import { providerLabel, providerSupported } from "./providers";

const STEPS = [
    { label: "Link", icon: CloudIcon },
    { label: "Mailboxes", icon: InboxIcon },
    { label: "Done", icon: SparklesIcon },
] as const;
type Step = 0 | 1 | 2;

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

export default function ConnectFlow({
    status,
    onFinished,
}: {
    status: CloudLinkStatus;
    onFinished: () => void;
}) {
    const [step, setStep] = React.useState<Step>(status.connected ? 1 : 0);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [nudged, setNudged] = React.useState(false);
    const [linked, setLinked] = React.useState(status.connected);
    const [enrolledCount, setEnrolledCount] = React.useState(0);
    // The status prop was read before the handshake, so the workspace name has
    // to come from the poll that completed it.
    const [orgName, setOrgName] = React.useState(status.link?.organization_name ?? "");

    const issue = step === 0 && !linked ? "Approve the code on Warmbly Cloud first" : null;
    React.useEffect(() => {
        if (!issue) setNudged(false);
    }, [issue]);

    const canReach = React.useCallback((target: Step) => (target === 0 ? true : linked), [linked]);
    const goTo = React.useCallback(
        (target: Step) => {
            if (target === step) return;
            if (target > step && !canReach(target)) {
                setNudged(true);
                return;
            }
            setDirection(target > step ? 1 : -1);
            setNudged(false);
            setStep(target);
        },
        [step, canReach],
    );

    return (
        <div className="px-4 py-5 md:px-8 md:py-6">
            <div className="rounded-lg border border-slate-200 bg-white overflow-hidden">
                <Stepper step={step} canReach={canReach} goTo={goTo} />
                <div className="overflow-hidden">
                    <AnimatePresence mode="wait" initial={false} custom={direction}>
                        <motion.div
                            key={step}
                            custom={direction}
                            variants={paneVariants}
                            initial="enter"
                            animate="center"
                            exit="exit"
                            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                            className="px-5 py-5 min-h-[320px]"
                        >
                            {step === 0 && (
                                <LinkStep
                                    status={status}
                                    linked={linked}
                                    orgName={orgName}
                                    onLinked={(name) => {
                                        setOrgName(name);
                                        setLinked(true);
                                        setTimeout(() => goTo(1), 650);
                                    }}
                                />
                            )}
                            {step === 1 && <MailboxesStep onCountChange={setEnrolledCount} />}
                            {step === 2 && <DoneStep orgName={orgName} enrolledCount={enrolledCount} />}
                        </motion.div>
                    </AnimatePresence>
                </div>
                <Footer
                    step={step}
                    issue={issue}
                    nudged={nudged}
                    onBack={() => goTo((step - 1) as Step)}
                    onNext={() => {
                        if (issue) {
                            setNudged(true);
                            return;
                        }
                        if (step < 2) goTo((step + 1) as Step);
                        else onFinished();
                    }}
                />
            </div>
        </div>
    );
}

function Stepper({ step, canReach, goTo }: { step: Step; canReach: (s: Step) => boolean; goTo: (s: Step) => void }) {
    return (
        <div className="px-5 pt-4 pb-3 border-b border-slate-200/70 flex items-center gap-2">
            {STEPS.map((s, idx) => {
                const i = idx as Step;
                const done = i < step;
                const active = i === step;
                const reachable = i <= step || canReach(i);
                return (
                    <React.Fragment key={s.label}>
                        <button
                            type="button"
                            disabled={!reachable}
                            onClick={() => goTo(i)}
                            className={`inline-flex items-center gap-1.5 text-[12px] transition-colors ${
                                active ? "text-slate-900 font-medium" : done ? "text-slate-700" : "text-slate-400"
                            } ${reachable ? "" : "cursor-not-allowed"}`}
                        >
                            <span
                                className={`relative size-5 rounded-full inline-flex items-center justify-center text-[10px] border transition-colors ${
                                    done ? "bg-sky-600 border-sky-600 text-white" : active ? "border-sky-600 text-sky-700" : "border-slate-300 text-slate-400"
                                }`}
                            >
                                <AnimatePresence mode="wait" initial={false}>
                                    {done ? (
                                        <motion.span key="check" initial={{ scale: 0.4, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} exit={{ scale: 0.4, opacity: 0 }}>
                                            <CheckIcon className="w-3 h-3" />
                                        </motion.span>
                                    ) : (
                                        <motion.span key="num" initial={{ scale: 0.4, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} exit={{ scale: 0.4, opacity: 0 }}>
                                            {i + 1}
                                        </motion.span>
                                    )}
                                </AnimatePresence>
                            </span>
                            {s.label}
                        </button>
                        {idx < STEPS.length - 1 && (
                            <span className="relative flex-1 h-px bg-slate-200 overflow-hidden">
                                <motion.span
                                    className="absolute inset-0 bg-sky-500"
                                    style={{ originX: 0 }}
                                    animate={{ scaleX: done ? 1 : 0 }}
                                    transition={{ duration: 0.3, ease: [0.22, 1, 0.36, 1] }}
                                />
                            </span>
                        )}
                    </React.Fragment>
                );
            })}
        </div>
    );
}

function Footer({ step, issue, nudged, onBack, onNext }: { step: Step; issue: string | null; nudged: boolean; onBack: () => void; onNext: () => void }) {
    return (
        <div className="px-5 py-3 border-t border-slate-200/70 flex items-center gap-3">
            <div className="flex-1 min-w-0">
                <AnimatePresence>
                    {nudged && issue && (
                        <motion.p
                            role="status"
                            initial={{ opacity: 0, y: 4 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: 4 }}
                            className="text-[12px] text-amber-700"
                        >
                            {issue}
                        </motion.p>
                    )}
                </AnimatePresence>
            </div>
            {step > 0 && (
                <button type="button" onClick={onBack} className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors">
                    Back
                </button>
            )}
            <button
                type="button"
                onClick={onNext}
                className={`h-7 px-2.5 rounded-md text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors ${
                    issue ? "bg-slate-300 cursor-not-allowed" : "bg-sky-600 hover:bg-sky-700"
                }`}
            >
                {step === 2 ? "Finish" : "Continue"}
                <ArrowRightIcon className="w-3.5 h-3.5" />
            </button>
        </div>
    );
}

// Step 1: the shared link card.
function LinkStep({ status, linked, orgName, onLinked }: { status: CloudLinkStatus; linked: boolean; orgName: string; onLinked: (orgName: string) => void }) {
    return <CloudLinkCard linked={linked} orgName={orgName} cloudUrl={status.default_cloud_url} onLinked={onLinked} />;
}

// Step 2: pick mailboxes.
function MailboxesStep({ onCountChange }: { onCountChange: (n: number) => void }) {
    const rows = useCloudLinkMailboxes();
    const enroll = useEnrollCloudLinkMailbox();
    const unenroll = useUnenrollCloudLinkMailbox();
    const [busy, setBusy] = React.useState<string | null>(null);

    React.useEffect(() => {
        onCountChange((rows.data ?? []).filter((r) => r.enrolled).length);
    }, [rows.data, onCountChange]);

    const flip = async (row: CloudLinkMailboxRow) => {
        setBusy(row.id);
        try {
            if (row.enrolled) {
                await unenroll.mutateAsync(row.id);
                toast.success(`${row.email} removed from the pool`);
            } else {
                await enroll.mutateAsync(row.id);
                toast.success(`${row.email} is now warming in the pool`);
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setBusy(null);
        }
    };

    return (
        <div className="space-y-4">
            <div>
                <h3 className="text-[14px] font-semibold text-slate-900">Choose the mailboxes to warm</h3>
                <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1">
                    Each enrolled mailbox is warmed by Warmbly Cloud from now on. Its local warmup stops; campaigns keep sending from here as usual.
                </p>
            </div>
            {rows.isLoading ? (
                <div className="py-8 flex justify-center text-slate-400">
                    <Loader2Icon className="w-4 h-4 animate-spin" />
                </div>
            ) : (rows.data ?? []).length === 0 ? (
                <p className="text-[12.5px] text-slate-500">No active mailboxes yet. Connect one under Mailboxes, then come back here.</p>
            ) : (
                <ul className="rounded-md border border-slate-200 divide-y divide-slate-200/70 overflow-hidden">
                    {(rows.data ?? []).map((row, i) => {
                        const supported = providerSupported(row.provider);
                        return (
                            <motion.li
                                key={row.id}
                                initial={{ opacity: 0, y: 6 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ delay: i * 0.03 }}
                                className="flex items-center gap-3 px-3 h-11 bg-white"
                            >
                                <div className="min-w-0 flex-1">
                                    <p className="text-[12.5px] text-slate-900 truncate">{row.email}</p>
                                    <p className="text-[11px] text-slate-400 truncate">
                                        {providerLabel(row.provider)}
                                        {!supported && " · connect with SMTP/IMAP to enroll"}
                                    </p>
                                </div>
                                {busy === row.id ? (
                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                                ) : (
                                    <Toggle on={row.enrolled} disabled={!supported} onChange={() => void flip(row)} />
                                )}
                            </motion.li>
                        );
                    })}
                </ul>
            )}
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Enrolling sends the mailbox's SMTP/IMAP credential to Warmbly Cloud, sealed in transit and at rest, and used only to send and read warmup mail.
                Nothing else in the mailbox is stored.
            </p>
        </div>
    );
}

function DoneStep({ orgName, enrolledCount }: { orgName: string; enrolledCount: number }) {
    return (
        <div className="flex flex-col items-center justify-center text-center gap-3 py-8">
            <motion.span
                initial={{ scale: 0.6, rotate: -12 }}
                animate={{ scale: 1, rotate: 0 }}
                transition={{ type: "spring", stiffness: 380, damping: 20 }}
                className="size-12 rounded-full bg-sky-50 text-sky-600 inline-flex items-center justify-center"
            >
                <FlameIcon className="w-6 h-6" />
            </motion.span>
            <div>
                <p className="text-[14px] font-semibold text-slate-900">
                    {enrolledCount === 0 ? "You are all set" : `${enrolledCount} mailbox${enrolledCount === 1 ? "" : "es"} warming in the pool`}
                </p>
                <p className="text-[12.5px] text-slate-500 mt-0.5 max-w-md">
                    {orgName ? `Linked to ${orgName}. ` : ""}
                    Warmup starts on the first slot of each mailbox's window and ramps daily. Health and volume show up on this page as they arrive.
                </p>
            </div>
        </div>
    );
}
