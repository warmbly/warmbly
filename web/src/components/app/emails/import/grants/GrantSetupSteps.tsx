// The setup of a new Microsoft admin grant: a Global Administrator approves
// Warmbly once. The popup lives in GrantImportWizard. Instruction and Crumbs
// are shared with the Google trusted-app steps.
import React from "react";
import { motion } from "framer-motion";
import { ArrowLeftIcon, ChevronRightIcon, ExternalLinkIcon, Loader2Icon, ShieldCheckIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import type { DomainGrant } from "@/lib/api/models/app/emails/MailboxSources";
import { Banner, SectionLabel } from "../parts";

const MS_ACCESS_POLICY_DOCS = "https://learn.microsoft.com/en-us/graph/auth-limit-mailbox-access";

function StepHeader({ provider, title, sub }: { provider: "google" | "microsoft"; title: string; sub: string }) {
    return (
        <div className="flex items-start gap-3">
            <ProviderLogo id={provider} size="xl" />
            <div className="min-w-0">
                <p className="text-[13.5px] font-medium text-slate-900">{title}</p>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">{sub}</p>
            </div>
        </div>
    );
}

export function Instruction({ n, children }: { n: number; children: React.ReactNode }) {
    return (
        <motion.li
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.03 + n * 0.04, duration: 0.18, ease: "easeOut" }}
            className="flex items-start gap-2.5"
        >
            <span className="size-5 rounded-full bg-white text-sky-700 ring-1 ring-inset ring-sky-600 inline-flex items-center justify-center text-[10.5px] font-semibold tabular-nums shrink-0 mt-px">
                {n}
            </span>
            <div className="min-w-0 flex-1 space-y-2 text-[12px] text-slate-700 leading-relaxed">{children}</div>
        </motion.li>
    );
}

export function Crumbs({ items }: { items: string[] }) {
    return (
        <span className="inline-flex flex-wrap items-center gap-1 align-middle">
            {items.map((c, i) => (
                <React.Fragment key={c}>
                    {i > 0 && <ChevronRightIcon className="w-3 h-3 text-slate-400" />}
                    <span className="h-[18px] px-1.5 rounded border border-slate-200 bg-slate-50 text-[11px] font-medium text-slate-700 inline-flex items-center whitespace-nowrap">
                        {c}
                    </span>
                </React.Fragment>
            ))}
        </span>
    );
}

function CancelSetup({ label, onClick }: { label: string; onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="h-6 -ml-1 px-1 rounded text-[11.5px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
        >
            <ArrowLeftIcon className="w-3 h-3" />
            {label}
        </button>
    );
}

/** The one step of a Microsoft organization: a Global Administrator approves Warmbly. */
export function MicrosoftConsentStep({
    consent,
    error,
    granted,
    onCancel,
}: {
    consent: { busy: boolean; start: () => Promise<void> };
    error: string | null;
    /** The organization this workspace just connected, if any. */
    granted: DomainGrant | null;
    onCancel?: () => void;
}) {
    return (
        <div className="p-4 space-y-4">
            {onCancel && <CancelSetup label="Back to connected organizations" onClick={onCancel} />}
            <StepHeader
                provider="microsoft"
                title="A Global Administrator approves Warmbly"
                sub="One approval covers the whole organization. Nobody else has to approve anything, and there are no passwords."
            />
            <ol className="space-y-3">
                <Instruction n={1}>
                    <p>
                        Click the button below. If you are not a Global Administrator, sign in with their account in the window that opens, or
                        have them open this screen.
                    </p>
                </Instruction>
                <Instruction n={2}>
                    <p>
                        Microsoft shows its consent screen for the whole organization. Review the permissions and click{" "}
                        <span className="font-medium text-slate-900">Accept</span>, once. The window then confirms who approved it, usually
                        without asking again, and closes.
                    </p>
                </Instruction>
            </ol>
            <div className="rounded-md border border-slate-200 p-3 space-y-1.5 text-[11.5px] text-slate-600 leading-relaxed">
                <SectionLabel>What Warmbly gets</SectionLabel>
                <p className="flex items-start gap-1.5">
                    <ShieldCheckIcon className="w-3 h-3 text-slate-400 mt-0.5 shrink-0" />
                    <span>Read, write and send mail for the mailboxes you pick next.</span>
                </p>
                <p className="flex items-start gap-1.5">
                    <ShieldCheckIcon className="w-3 h-3 text-slate-400 mt-0.5 shrink-0" />
                    <span>Read the organization&apos;s user list, so you can pick those mailboxes.</span>
                </p>
                <p>
                    To hold Warmbly to specific mailboxes, an Exchange administrator can add an{" "}
                    <a
                        href={MS_ACCESS_POLICY_DOCS}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-0.5 text-sky-700 underline decoration-sky-300 hover:decoration-sky-600"
                    >
                        application access policy
                        <ExternalLinkIcon className="w-2.5 h-2.5" />
                    </a>
                    .
                </p>
            </div>
            {granted ? (
                <Banner tone="sky" title="Approved">
                    The organization is connected. Continue to pick its mailboxes.
                </Banner>
            ) : (
                <button
                    type="button"
                    onClick={() => void consent.start()}
                    disabled={consent.busy}
                    className="w-full h-9 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12.5px] font-medium inline-flex items-center justify-center gap-2 transition-colors disabled:opacity-60"
                >
                    {consent.busy ? (
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                        <span className="size-5 rounded bg-white inline-flex items-center justify-center">
                            <ProviderLogo id="microsoft" size="xs" framed={false} />
                        </span>
                    )}
                    {consent.busy ? "Waiting for the administrator…" : "Open Microsoft's consent screen"}
                </button>
            )}
            {error && !granted && (
                <Banner tone="red" title="The organization is not connected yet">
                    {error}
                </Banner>
            )}
        </div>
    );
}
