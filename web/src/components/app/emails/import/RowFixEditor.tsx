// RowFixEditor: a failed or sign-in row's new password or servers, saved with PATCH
// /emails/imports/:id/rows/:line, which re-seals the row and queues it again.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronDownIcon, Loader2Icon, RotateCcwIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import type { ImportRow, RowFix, RowFixLeg } from "@/lib/api/models/app/emails/MailboxImport";
import { defaultImapSecurity, defaultSmtpSecurity, validPort, type MailSecurity } from "@/lib/api/models/app/emails/Service";
import { useFixMailboxImportRow } from "@/lib/api/hooks/app/emails/useMailboxImportActions";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import SecuritySelect from "@/components/app/emails/SecuritySelect";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { mailHostLabel } from "@/lib/mailHost";
import { cn } from "@/lib/utils";
import { isGoogleServer, looksLikeGoogleAppPassword } from "./importFields";

// Hosts that take only an app password over IMAP and SMTP.
const APP_PASSWORD_HOSTS = new Set(["google_workspace", "gmail", "yahoo", "aol", "icloud", "fastmail", "yandex"]);

interface LegDraft {
    host: string;
    port: number;
    security: MailSecurity | null;
    username: string;
}

const emptyLeg: LegDraft = { host: "", port: NaN, security: null, username: "" };

function legFix(d: LegDraft): RowFixLeg | undefined {
    const out: RowFixLeg = {};
    if (d.host.trim()) out.host = d.host.trim();
    if (validPort(d.port)) out.port = d.port;
    if (d.security) out.security = d.security;
    if (d.username.trim()) out.username = d.username.trim();
    return Object.keys(out).length > 0 ? out : undefined;
}

export default function RowFixEditor({
    importId,
    row,
    onClose,
}: {
    importId: string;
    row: ImportRow;
    onClose: () => void;
}) {
    const fix = useFixMailboxImportRow(importId);
    const selfHosted = useAuthConfig().data?.self_hosted === true;
    const appPassword = APP_PASSWORD_HOSTS.has(row.mail_host);
    const [secret, setSecret] = React.useState("");
    const [username, setUsername] = React.useState("");
    const [advanced, setAdvanced] = React.useState(false);
    const [smtp, setSmtp] = React.useState<LegDraft>(emptyLeg);
    const [imap, setImap] = React.useState<LegDraft>(emptyLeg);

    const body: RowFix = {};
    if (secret) body[appPassword ? "app_password" : "password"] = secret;
    if (username.trim()) body.username = username.trim();
    const smtpFix = legFix(smtp);
    const imapFix = legFix(imap);
    if (smtpFix) body.smtp = smtpFix;
    if (imapFix) body.imap = imapFix;
    const empty = Object.keys(body).length === 0;
    const googleRow = smtp.host.trim() ? isGoogleServer(smtp.host) : row.mail_host === "google_workspace" || row.mail_host === "gmail";
    const notAppPassword = googleRow && !!secret && !looksLikeGoogleAppPassword(secret);

    async function save() {
        if (empty || notAppPassword || fix.isPending) return;
        try {
            await fix.mutateAsync({ line: row.line, fix: body });
            toast.success(`${row.email || `Line ${row.line}`} is queued again`);
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    const host = mailHostLabel(row.mail_host);

    return (
        <div
            className="px-3 py-3 bg-slate-50/60 border-t border-slate-100 space-y-2.5"
            onKeyDown={(e) => {
                if (e.key === "Enter" && (e.target as HTMLElement).tagName === "INPUT") {
                    e.preventDefault();
                    void save();
                }
            }}
        >
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                <div>
                    <Label>{appPassword ? "New app password" : "New password"}</Label>
                    <TextInput
                        value={secret}
                        onChange={setSecret}
                        type="password"
                        autoComplete="new-password"
                        autoFocus
                        placeholder={appPassword ? `The app password ${host || "the host"} gave you` : "The mailbox password"}
                        className="w-full"
                    />
                </div>
                <div>
                    <Label>Sign-in username</Label>
                    <TextInput
                        value={username}
                        onChange={setUsername}
                        autoComplete="off"
                        placeholder={row.email || "Only if it is not the address"}
                        className="w-full"
                    />
                </div>
            </div>

            <button
                type="button"
                onClick={() => setAdvanced((v) => !v)}
                aria-expanded={advanced}
                className="inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:text-slate-900"
            >
                <ChevronDownIcon className={cn("w-3 h-3 transition-transform", advanced && "rotate-180")} />
                Servers
            </button>
            <AnimatePresence initial={false}>
                {advanced && (
                    <motion.div
                        key="servers"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                        className="overflow-hidden"
                    >
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-0.5">
                            <LegFields
                                title="SMTP"
                                draft={smtp}
                                onChange={setSmtp}
                                hostPlaceholder="smtp.example.com"
                                defaultSecurity={defaultSmtpSecurity}
                                selfHosted={selfHosted}
                            />
                            <LegFields
                                title="IMAP"
                                draft={imap}
                                onChange={setImap}
                                hostPlaceholder="imap.example.com"
                                defaultSecurity={defaultImapSecurity}
                                selfHosted={selfHosted}
                            />
                        </div>
                        <p className="mt-1.5 text-[11px] text-slate-500">Anything left blank keeps what the row already has.</p>
                    </motion.div>
                )}
            </AnimatePresence>

            <div className="flex items-center gap-2">
                <span className={cn("text-[11px] min-w-0 truncate", notAppPassword ? "text-rose-600" : "text-slate-500")}>
                    {notAppPassword
                        ? "Google needs the 16-letter app password, not the account password."
                        : empty
                          ? "Enter a new password or change a server to retry this row."
                          : "The row is checked again right after saving."}
                </span>
                <button
                    type="button"
                    onClick={onClose}
                    className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0"
                >
                    Cancel
                </button>
                <button
                    type="button"
                    onClick={() => void save()}
                    disabled={empty || notAppPassword || fix.isPending}
                    className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                >
                    {fix.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RotateCcwIcon className="w-3 h-3" />}
                    Save and retry
                </button>
            </div>
        </div>
    );
}

function LegFields({
    title,
    draft,
    onChange,
    hostPlaceholder,
    defaultSecurity,
    selfHosted,
}: {
    title: string;
    draft: LegDraft;
    onChange: (d: LegDraft) => void;
    hostPlaceholder: string;
    defaultSecurity: (port: number) => MailSecurity;
    selfHosted: boolean;
}) {
    // Until one is picked the security follows the port, and only a picked one is sent.
    const shown = draft.security ?? defaultSecurity(validPort(draft.port) ? draft.port : 0);
    return (
        <div className="space-y-1.5 min-w-0">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{title}</div>
            <div className="flex items-center gap-1.5">
                <TextInput
                    value={draft.host}
                    onChange={(v) => onChange({ ...draft, host: v })}
                    placeholder={hostPlaceholder}
                    className="flex-1"
                />
                <NumberInput
                    value={draft.port}
                    onChange={(v) => onChange({ ...draft, port: v > 0 ? v : NaN })}
                    min={0}
                    max={65535}
                    placeholder="Port"
                    className="w-24 shrink-0"
                />
            </div>
            <SecuritySelect
                value={shown}
                host={draft.host}
                selfHosted={selfHosted}
                onChange={(v) => onChange({ ...draft, security: v })}
            />
            <TextInput
                value={draft.username}
                onChange={(v) => onChange({ ...draft, username: v })}
                placeholder={`${title} username, if different`}
                autoComplete="off"
                className="w-full"
            />
        </div>
    );
}
