// Replacement-credentials dialog for an SMTP/IMAP mailbox whose password
// changed (issue #274). Same fields and validation as the connect form in
// AddEmailModal, minus name/email, which a reconnect never changes. The
// backend verifies the credentials against a live worker before storing, then
// reactivates the mailbox and clears its credential errors.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, InboxIcon, KeyRoundIcon, Loader2Icon, SendIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";

import { TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import updateEmailCredentials from "@/lib/api/client/app/emails/updateEmailCredentials";
import {
    defaultImapSecurity,
    defaultSmtpSecurity,
    validPort,
    type MailSecurity,
} from "@/lib/api/models/app/emails/Service";
import SecuritySelect from "@/components/app/emails/SecuritySelect";

export default function UpdateCredentialsDialog({
    mailboxId,
    mailboxEmail,
    open,
    onClose,
}: {
    mailboxId: string;
    mailboxEmail: string;
    open: boolean;
    onClose: () => void;
}) {
    const qc = useQueryClient();

    const [imapHost, setImapHost] = React.useState("");
    const [imapPort, setImapPort] = React.useState("993");
    const [imapUser, setImapUser] = React.useState(mailboxEmail);
    const [imapPass, setImapPass] = React.useState("");

    const [smtpHost, setSmtpHost] = React.useState("");
    const [smtpPort, setSmtpPort] = React.useState("587");
    const [smtpUser, setSmtpUser] = React.useState(mailboxEmail);
    const [smtpPass, setSmtpPass] = React.useState("");

    const [imapSecurity, setImapSecurity] = React.useState<MailSecurity>("tls");
    const [smtpSecurity, setSmtpSecurity] = React.useState<MailSecurity>("starttls");
    const [sameCreds, setSameCreds] = React.useState(true);
    const [submitting, setSubmitting] = React.useState(false);

    // The port implies the mode until the user picks one by hand, matching the
    // connect form.
    const imapSecurityTouched = React.useRef(false);
    const smtpSecurityTouched = React.useRef(false);
    React.useEffect(() => {
        if (!imapSecurityTouched.current) setImapSecurity(defaultImapSecurity(Number(imapPort)));
    }, [imapPort]);
    React.useEffect(() => {
        if (!smtpSecurityTouched.current) setSmtpSecurity(defaultSmtpSecurity(Number(smtpPort)));
    }, [smtpPort]);

    // Reset when reopened so a cancelled attempt never leaks a typed password.
    React.useEffect(() => {
        if (open) {
            setImapHost("");
            setImapPort("993");
            setImapUser(mailboxEmail);
            setImapPass("");
            setSmtpHost("");
            setSmtpPort("587");
            setSmtpUser(mailboxEmail);
            setSmtpPass("");
            imapSecurityTouched.current = false;
            smtpSecurityTouched.current = false;
            setImapSecurity("tls");
            setSmtpSecurity("starttls");
            setSameCreds(true);
            setSubmitting(false);
        }
    }, [open, mailboxEmail]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, onClose]);

    function valid() {
        if (!imapHost.trim() || !imapPort.trim() || !imapUser.trim() || !imapPass) return false;
        if (!smtpHost.trim() || !smtpPort.trim()) return false;
        if (!sameCreds && (!smtpUser.trim() || !smtpPass)) return false;
        // Any routable port; the security mode carries how to connect.
        if (!validPort(Number(smtpPort)) || !validPort(Number(imapPort))) return false;
        return true;
    }

    async function submit() {
        if (submitting || !valid()) return;
        setSubmitting(true);
        const smtp = sameCreds
            ? { username: imapUser.trim(), password: imapPass, host: smtpHost.trim(), port: Number(smtpPort), security: smtpSecurity }
            : { username: smtpUser.trim(), password: smtpPass, host: smtpHost.trim(), port: Number(smtpPort), security: smtpSecurity };
        try {
            await toast.promise(
                updateEmailCredentials(mailboxId, smtp, {
                    username: imapUser.trim(),
                    password: imapPass,
                    host: imapHost.trim(),
                    port: Number(imapPort),
                    security: imapSecurity,
                }),
                {
                    loading: "Verifying credentials…",
                    success: "Credentials updated. The mailbox is back online.",
                    error: (e: AppError) => buildError(e),
                },
            );
            qc.invalidateQueries({ queryKey: ["emails", "list"] });
            qc.invalidateQueries({ queryKey: ["analytics", "accounts"] });
            onClose();
        } catch {
            /* surfaced by toast */
        } finally {
            setSubmitting(false);
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={onClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[480px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
                    >
                        <div className="h-12 px-3 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Mailbox</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12px] text-slate-600 truncate">Update credentials for {mailboxEmail}</span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="flex-1 min-h-0 overflow-y-auto">
                            <Section title="IMAP" sub="Incoming, usually 993" icon={<InboxIcon className="w-3.5 h-3.5" />}>
                                <Field label="Server">
                                    <HostPortInput host={imapHost} onHost={setImapHost} hostPlaceholder="imap.example.com" port={imapPort} onPort={setImapPort} portPlaceholder="993" />
                                </Field>
                                <Field label="Security">
                                    <SecuritySelect
                                        value={imapSecurity}
                                        onChange={(v) => {
                                            imapSecurityTouched.current = true;
                                            setImapSecurity(v);
                                        }}
                                    />
                                </Field>
                                <Field label="Username">
                                    <TextInput value={imapUser} onChange={setImapUser} placeholder={mailboxEmail} />
                                </Field>
                                <Field label="Password">
                                    <TextInput value={imapPass} onChange={setImapPass} placeholder="New app password" type="password" />
                                </Field>
                            </Section>

                            <Section title="SMTP" sub="Outgoing, 465 or 587" icon={<SendIcon className="w-3.5 h-3.5" />}>
                                <Field label="Server">
                                    <HostPortInput host={smtpHost} onHost={setSmtpHost} hostPlaceholder="smtp.example.com" port={smtpPort} onPort={setSmtpPort} portPlaceholder="587" />
                                </Field>
                                <Field label="Security">
                                    <SecuritySelect
                                        value={smtpSecurity}
                                        onChange={(v) => {
                                            smtpSecurityTouched.current = true;
                                            setSmtpSecurity(v);
                                        }}
                                    />
                                </Field>
                                <label className="flex items-center gap-2 pl-[76px] pt-0.5 cursor-pointer">
                                    <input
                                        type="checkbox"
                                        checked={sameCreds}
                                        onChange={(e) => setSameCreds(e.target.checked)}
                                        className="size-3.5 rounded border-slate-300 accent-slate-900"
                                    />
                                    <span className="text-[11.5px] text-slate-600">Use the same login as IMAP</span>
                                </label>
                                <AnimatePresence initial={false}>
                                    {!sameCreds && (
                                        <motion.div
                                            key="smtp-creds"
                                            initial={{ height: 0, opacity: 0 }}
                                            animate={{ height: "auto", opacity: 1 }}
                                            exit={{ height: 0, opacity: 0 }}
                                            transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                                            className="overflow-hidden"
                                        >
                                            <div className="space-y-2 pt-2">
                                                <Field label="Username">
                                                    <TextInput value={smtpUser} onChange={setSmtpUser} placeholder={mailboxEmail} />
                                                </Field>
                                                <Field label="Password">
                                                    <TextInput value={smtpPass} onChange={setSmtpPass} placeholder="New app password" type="password" />
                                                </Field>
                                            </div>
                                        </motion.div>
                                    )}
                                </AnimatePresence>
                            </Section>
                        </div>

                        <div className="px-4 py-2.5 border-t border-slate-200 bg-slate-50/60 flex items-center gap-2 min-w-0 shrink-0">
                            <div className="flex items-center gap-1.5 text-[11px] text-slate-500 min-w-0 flex-1">
                                <KeyRoundIcon className="w-3 h-3 shrink-0" />
                                <span className="truncate">Verified against your server before saving.</span>
                            </div>
                            <motion.button
                                type="button"
                                onClick={submit}
                                disabled={!valid() || submitting}
                                whileTap={valid() && !submitting ? { scale: 0.97 } : undefined}
                                className="shrink-0 h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors bg-slate-900 hover:bg-slate-800 text-white disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                {submitting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                                Update credentials
                            </motion.button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Section({ title, sub, icon, children }: { title: string; sub: string; icon: React.ReactNode; children: React.ReactNode }) {
    return (
        <div className="px-4 py-3 border-b border-slate-200/60 last:border-b-0 min-w-0">
            <div className="flex items-center gap-1.5 mb-2 min-w-0">
                <span className="text-slate-500 shrink-0">{icon}</span>
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">{title}</span>
                <div className="h-3 w-px bg-slate-200 shrink-0" />
                <span className="text-[11.5px] text-slate-500 truncate min-w-0">{sub}</span>
            </div>
            <div className="space-y-2 min-w-0">{children}</div>
        </div>
    );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center gap-3 min-w-0">
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-16 shrink-0">{label}</span>
            <div className="flex-1 min-w-0">{children}</div>
        </div>
    );
}

// One bordered field holding host (flex) and port (fixed) with a hairline
// divider, same as the connect form's server rows.
function HostPortInput({
    host,
    onHost,
    hostPlaceholder,
    port,
    onPort,
    portPlaceholder,
}: {
    host: string;
    onHost: (v: string) => void;
    hostPlaceholder: string;
    port: string;
    onPort: (v: string) => void;
    portPlaceholder: string;
}) {
    return (
        <div className="flex items-stretch h-7 rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors min-w-0 overflow-hidden">
            <input
                value={host}
                onChange={(e) => onHost(e.target.value)}
                placeholder={hostPlaceholder}
                className="flex-1 min-w-0 px-2.5 bg-transparent outline-none text-[12.5px] text-slate-900 placeholder:text-slate-400"
            />
            <div className="w-px bg-slate-200 shrink-0" />
            <input
                value={port}
                onChange={(e) => onPort(e.target.value)}
                placeholder={portPlaceholder}
                inputMode="numeric"
                className="w-14 shrink-0 px-2 bg-slate-50/60 outline-none text-[12.5px] text-slate-900 placeholder:text-slate-400 tabular-nums text-center"
            />
        </div>
    );
}
