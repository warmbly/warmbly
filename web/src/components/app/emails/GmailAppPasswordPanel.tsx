// GmailAppPasswordPanel — the Gmail path of the connect modal.
//
// One Gmail or Google Workspace mailbox over IMAP and SMTP with an app
// password; per-mailbox Google sign-in is deployment-dependent, and a whole Workspace
// domain goes through an admin grant instead. The server settings never change, so
// the only things a person has to produce are the app password and the
// address, and the panel walks them to those in three steps: turn on 2-Step
// Verification, create the app password, connect. Steps slide like the rest
// of the multi-step flows, and nothing skips ahead of an incomplete one.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckIcon,
    ExternalLinkIcon,
    KeyRoundIcon,
    Loader2Icon,
    ShieldCheckIcon,
    SparklesIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";

import { TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import addEmail from "@/lib/api/client/app/emails/addEmail";
import { capture } from "@/lib/productAnalytics";
import { cn } from "@/lib/utils";

const EASE = [0.32, 0.72, 0, 1] as const;

/** Gmail's own servers. The same for personal accounts and Workspace. */
// 587 with STARTTLS rather than 465: Google offers both, and many hosts block
// outbound 465 while 587 stays open.
const GMAIL_SMTP = { host: "smtp.gmail.com", port: 587, security: "starttls", securityLabel: "STARTTLS" } as const;
const GMAIL_IMAP = { host: "imap.gmail.com", port: 993, security: "tls", securityLabel: "SSL / TLS" } as const;

/** Google shows an app password as four groups of four letters. */
const APP_PASSWORD_LENGTH = 16;

const STEPS = ["2-Step Verification", "App password", "Connect"] as const;

function normalizeAppPassword(raw: string): string {
    return raw.replace(/\s+/g, "");
}

export default function GmailAppPasswordPanel({
    onDone,
    onError,
    initialEmail = "",
}: {
    onDone: () => void;
    /** A refused connect (a full allowance) gets its own dialog upstairs. */
    onError: (e: unknown) => void;
    /** An address typed earlier, e.g. a personal Gmail steered here from Google sign-in. */
    initialEmail?: string;
}) {
    const [step, setStep] = React.useState(0);
    const [dir, setDir] = React.useState<1 | -1>(1);
    const go = (next: number) => {
        setDir(next > step ? 1 : -1);
        setStep(next);
    };

    return (
        <div className="flex flex-col min-h-0">
            <div className="px-4 py-2 border-b border-slate-200/60 bg-sky-50/60 flex items-center gap-2 text-[11.5px] text-sky-800">
                <SparklesIcon className="w-3.5 h-3.5 text-sky-600 shrink-0" />
                <span className="min-w-0 truncate">
                    An app password connects any Gmail or Workspace mailbox in about two minutes, with no Google consent screen.
                </span>
            </div>
            <Progress step={step} />
            <div className="relative overflow-hidden">
                <AnimatePresence mode="wait" initial={false} custom={dir}>
                    <motion.div
                        key={step}
                        custom={dir}
                        initial={{ opacity: 0, x: dir * 16 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: dir * -16 }}
                        transition={{ duration: 0.18, ease: EASE }}
                    >
                        {step === 0 && <TwoStepStep onNext={() => go(1)} />}
                        {step === 1 && <AppPasswordStep onBack={() => go(0)} onNext={() => go(2)} />}
                        {step === 2 && <ConnectStep onBack={() => go(1)} onDone={onDone} onError={onError} initialEmail={initialEmail} />}
                    </motion.div>
                </AnimatePresence>
            </div>
        </div>
    );
}

function Progress({ step }: { step: number }) {
    return (
        <div className="px-4 pt-3 pb-2 border-b border-slate-200/60">
            <div className="flex items-center gap-1.5">
                {STEPS.map((label, i) => (
                    <div key={label} className="flex-1 min-w-0">
                        <div
                            className={cn(
                                "h-1 rounded-full transition-colors",
                                i < step ? "bg-sky-600" : i === step ? "bg-sky-400" : "bg-slate-200",
                            )}
                        />
                        <div
                            className={cn(
                                "mt-1.5 text-[10px] uppercase tracking-[0.14em] font-medium truncate",
                                i === step ? "text-slate-900" : i < step ? "text-sky-700" : "text-slate-400",
                            )}
                        >
                            {label}
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}

function TwoStepStep({ onNext }: { onNext: () => void }) {
    return (
        <StepFrame
            title="Turn on 2-Step Verification"
            sub="Google only issues app passwords on accounts that have it on."
            footer={
                <>
                    <span className="text-[11px] text-slate-500 min-w-0 truncate">Already on? Just continue.</span>
                    <NextButton onClick={onNext}>It's on, continue</NextButton>
                </>
            }
        >
            <ol className="space-y-2">
                <Step n={1}>
                    Open the Google account's security page and find <strong className="font-medium text-slate-900">2-Step Verification</strong> under "How you sign in to Google".
                </Step>
                <Step n={2}>
                    Turn it on with a phone prompt, an authenticator app or a text message. A security key on its own is not enough: Google needs one of the other methods present before it offers app passwords.
                </Step>
            </ol>
            <ExternalButton href="https://myaccount.google.com/security">Open Google security settings</ExternalButton>
            <Note>
                On Google Workspace an administrator can require or block this for the organization. An account enrolled in Advanced Protection cannot have an app password at all.
            </Note>
        </StepFrame>
    );
}

function AppPasswordStep({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
    return (
        <StepFrame
            title="Create an app password"
            sub="A 16-letter password Google issues for one app. Warmbly stores it encrypted."
            footer={
                <>
                    <BackButton onClick={onBack} />
                    <NextButton onClick={onNext}>I have it, continue</NextButton>
                </>
            }
        >
            <ol className="space-y-2">
                <Step n={1}>Open the app passwords page. Google asks you to sign in again.</Step>
                <Step n={2}>
                    Type <strong className="font-medium text-slate-900">Warmbly</strong> as the app name and press <strong className="font-medium text-slate-900">Create</strong>.
                </Step>
                <Step n={3}>
                    Copy the 16 characters it shows. Google shows them once; the spaces between the groups do not matter.
                </Step>
            </ol>
            <ExternalButton href="https://myaccount.google.com/apppasswords">Open app passwords</ExternalButton>
            <Note>
                No such page? 2-Step Verification is still off, or on Workspace the administrator has blocked app passwords under Security, Less secure apps. Nothing to turn on for IMAP: Google removed that setting in January 2025.
            </Note>
        </StepFrame>
    );
}

function ConnectStep({
    onBack,
    onDone,
    onError,
    initialEmail,
}: {
    onBack: () => void;
    onDone: () => void;
    onError: (e: unknown) => void;
    initialEmail: string;
}) {
    const [name, setName] = React.useState("");
    const [email, setEmail] = React.useState(initialEmail);
    const [password, setPassword] = React.useState("");
    const [submitting, setSubmitting] = React.useState(false);

    const cleaned = normalizeAppPassword(password);
    const lengthOff = cleaned.length > 0 && cleaned.length !== APP_PASSWORD_LENGTH;
    const missing = [
        !name.trim() && "a name",
        !email.trim().includes("@") && "the address",
        !cleaned && "the app password",
    ].filter((m): m is string => Boolean(m));
    const valid = missing.length === 0;

    async function submit() {
        if (submitting || !valid) return;
        setSubmitting(true);
        const address = email.trim();
        try {
            await toast.promise(
                addEmail({
                    name: name.trim(),
                    email: address,
                    imap: { username: address, password: cleaned, host: GMAIL_IMAP.host, port: GMAIL_IMAP.port, security: GMAIL_IMAP.security },
                    smtp: { username: address, password: cleaned, host: GMAIL_SMTP.host, port: GMAIL_SMTP.port, security: GMAIL_SMTP.security },
                }),
                {
                    loading: "Checking the app password with Google…",
                    success: "Mailbox connected",
                    error: (e: AppError) => buildError(e),
                },
            );
            capture("mailbox_connected", { provider: "gmail", method: "app_password" });
            onDone();
        } catch (e) {
            onError(e);
        } finally {
            setSubmitting(false);
        }
    }

    return (
        <StepFrame
            title="Connect the mailbox"
            sub="The address, the app password, and nothing else to type."
            footer={
                <>
                    <BackButton onClick={onBack} />
                    <span className="flex-1 min-w-0 text-[11px] text-slate-500 truncate">
                        {valid ? "Verified with Google before saving." : `Still needed: ${missing.join(", ")}.`}
                    </span>
                    <motion.button
                        type="button"
                        onClick={submit}
                        disabled={!valid || submitting}
                        whileTap={valid && !submitting ? { scale: 0.97 } : undefined}
                        className="shrink-0 h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors bg-slate-900 hover:bg-slate-800 text-white disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        {submitting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                        Connect
                    </motion.button>
                </>
            }
        >
            <div className="space-y-2">
                <Field label="Name">
                    <TextInput value={name} onChange={setName} placeholder="Alex Rivera" />
                </Field>
                <Field label="Email">
                    <TextInput value={email} onChange={setEmail} placeholder="alex@gmail.com or alex@yourdomain.com" />
                </Field>
                <Field label="App password">
                    <TextInput
                        value={password}
                        onChange={setPassword}
                        placeholder="xxxx xxxx xxxx xxxx"
                        type="password"
                        onKeyDown={(e) => {
                            if (e.key === "Enter") void submit();
                        }}
                    />
                </Field>
                {lengthOff && (
                    <p className="pl-[76px] text-[11.5px] text-amber-700">
                        Google app passwords are {APP_PASSWORD_LENGTH} letters. This looks like something else, maybe the account password. It is checked on connect either way.
                    </p>
                )}
            </div>

            <div className="rounded-md border border-slate-200 overflow-hidden">
                <div className="px-3 h-8 flex items-center gap-1.5 border-b border-slate-200 bg-slate-50">
                    <KeyRoundIcon className="w-3 h-3 text-slate-500" />
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Server settings, set for you</span>
                </div>
                {[
                    { label: "SMTP", ...GMAIL_SMTP },
                    { label: "IMAP", ...GMAIL_IMAP },
                ].map((s) => (
                    <div key={s.label} className="px-3 py-2 flex items-center gap-3 text-[12px] border-b border-slate-100 last:border-b-0">
                        <span className="w-10 shrink-0 text-slate-500 font-medium">{s.label}</span>
                        <span className="font-mono text-slate-900 truncate">{s.host}</span>
                        <span className="font-mono tabular-nums text-slate-600">:{s.port}</span>
                        <span className="ml-auto text-slate-500 shrink-0">{s.securityLabel}</span>
                    </div>
                ))}
                <div className="px-3 py-2 text-[11.5px] text-slate-500 border-t border-slate-100">
                    The address must be the one the Google account signs in with, not an alias, and the app password must come from that same account. Deleting it in the Google account disconnects the mailbox.
                </div>
            </div>
        </StepFrame>
    );
}

function StepFrame({
    title,
    sub,
    children,
    footer,
}: {
    title: string;
    sub: string;
    children: React.ReactNode;
    footer: React.ReactNode;
}) {
    return (
        <div>
            <div className="px-4 pt-4 pb-3 space-y-3.5">
                <div className="flex items-start gap-3">
                    <div className="size-9 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0">
                        <ShieldCheckIcon className="w-4 h-4 text-slate-700" />
                    </div>
                    <div className="min-w-0">
                        <div className="text-[13.5px] font-medium text-slate-900">{title}</div>
                        <div className="text-[11.5px] text-slate-500">{sub}</div>
                    </div>
                </div>
                {children}
            </div>
            <div className="px-4 py-2.5 border-t border-slate-200 bg-slate-50/60 flex items-center gap-2 min-w-0 sticky bottom-0">
                {footer}
            </div>
        </div>
    );
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
    return (
        <li className="flex items-start gap-2.5">
            <span className="size-4 mt-0.5 shrink-0 rounded-full bg-slate-100 text-slate-600 text-[10px] font-medium inline-flex items-center justify-center tabular-nums">
                {n}
            </span>
            <span className="text-[12.5px] text-slate-700 leading-[1.5]">{children}</span>
        </li>
    );
}

function Note({ children }: { children: React.ReactNode }) {
    return <p className="text-[11.5px] text-slate-500 leading-[1.5]">{children}</p>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center gap-3 min-w-0">
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-16 shrink-0">{label}</span>
            <div className="flex-1 min-w-0">{children}</div>
        </div>
    );
}

function ExternalButton({ href, children }: { href: string; children: React.ReactNode }) {
    return (
        <a
            href={href}
            target="_blank"
            rel="noreferrer"
            className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 text-[12.5px] text-slate-700 hover:bg-slate-50 transition-colors"
        >
            <ExternalLinkIcon className="w-3.5 h-3.5" />
            {children}
        </a>
    );
}

function BackButton({ onClick }: { onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="shrink-0 h-7 px-2 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
        >
            <ArrowLeftIcon className="w-3 h-3" />
            Back
        </button>
    );
}

function NextButton({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
    return (
        <motion.button
            type="button"
            onClick={onClick}
            whileTap={{ scale: 0.97 }}
            className="ml-auto shrink-0 h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
        >
            {children}
            <ArrowRightIcon className="w-3 h-3" />
        </motion.button>
    );
}
