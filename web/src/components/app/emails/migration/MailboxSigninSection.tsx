// The mailbox drawer's Sign-in section: how the mailbox signs in now, and only
// the switches the server says can succeed for it. Every switch keeps the
// mailbox, its history, campaigns and warmup.
import React from "react";
import toast from "react-hot-toast/headless";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    ChevronDownIcon,
    KeyRoundIcon,
    Loader2Icon,
    RefreshCwIcon,
    ShieldCheckIcon,
    SparklesIcon,
} from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import useMailboxSignin from "@/lib/api/hooks/app/emails/useMailboxSignin";
import { useStartVendorReconnect } from "@/lib/api/hooks/app/emails/useMailboxVendors";
import { MoveError, useMoveToGrant } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { reconnectOpen, vendorLabel, type MailboxSignin, type ReconnectStatus } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";
import MailboxImportDialog from "../import/MailboxImportDialog";
import { BlockedByAdminHint, TrustedAppSteps } from "../GoogleWorkspaceSigninPanel";
import AppPasswordSwitchForm from "./AppPasswordSwitchForm";
import useGoogleReconnect from "./useGoogleReconnect";
import { useConfirm } from "@/hooks/context/confirm";

type Mailbox = { id: string; email: string };

const STATUS_COPY: Record<ReconnectStatus, string> = {
    queued: "is about to start",
    trusting: "is adding Warmbly as a trusted app on the domain",
    signing_in: "is signing in as the mailbox",
    connected: "reconnected it",
    failed: "could not reconnect it",
};

/** What the mailbox signs in with, in a line a person can read. */
function methodCopy(s: MailboxSignin): { title: string; detail: string; tone: "ok" | "older" | "plain" } {
    switch (s.method) {
        case "google_signin":
            if (s.full_mail_access) {
                return { title: "Google sign-in", detail: "Signed in on its own with full mail access, so warmup engagement runs over IMAP.", tone: "ok" };
            }
            return {
                title: "Google sign-in, older permissions",
                detail: "Signed in on its own with the permissions Warmbly asked for before. It keeps working; warmup engagement runs through the Gmail API.",
                tone: "older",
            };
        case "google_grant":
            return {
                title: "Domain grant (legacy)",
                detail: "Reached through an administrator's domain-wide grant. It keeps working; new grants are no longer offered.",
                tone: "older",
            };
        case "app_password":
            return { title: "App password", detail: "Signs in to Gmail's IMAP and SMTP with an app password.", tone: "plain" };
    }
    return { title: "", detail: "", tone: "plain" };
}

/** The live standing of the mailbox's last vendor reconnect. */
function SwitchStatus({ s }: { s: MailboxSignin }) {
    const sw = s.switch;
    if (!sw) return null;
    const vendor = vendorLabel(s.options.vendor?.vendor) || "The vendor";
    const open = reconnectOpen(sw.status);
    // A finished reconnect says so for a day; the method line carries it after that.
    if (sw.status === "connected" && Date.now() - new Date(sw.at).getTime() > 86_400_000) return null;
    return (
        <div
            role="status"
            className={cn(
                "rounded-md border px-3 py-2 flex items-start gap-2 text-[11.5px] leading-relaxed",
                sw.status === "failed" && "border-rose-200 bg-rose-50 text-rose-800",
                sw.status === "connected" && "border-emerald-200 bg-emerald-50 text-emerald-800",
                open && "border-sky-200 bg-sky-50 text-sky-800",
            )}
        >
            {open && <Loader2Icon className="w-3.5 h-3.5 mt-0.5 shrink-0 animate-spin" />}
            {sw.status === "failed" && <AlertCircleIcon className="w-3.5 h-3.5 mt-0.5 shrink-0" />}
            {sw.status === "connected" && <CheckCircle2Icon className="w-3.5 h-3.5 mt-0.5 shrink-0" />}
            <div className="min-w-0">
                <p>
                    {vendor} {STATUS_COPY[sw.status]}
                    {sw.automatic ? ", on its own" : ""}. <span className="opacity-70">{timeAgo(sw.at)}</span>
                </p>
                {sw.status === "failed" && (
                    <p className="mt-0.5">
                        {sw.message || "The vendor gave no reason."} The mailbox keeps working on its current sign-in.
                        {sw.automatic ? " Warmbly tries again in a week." : ""}
                    </p>
                )}
            </div>
        </div>
    );
}

function OptionRow({
    icon,
    title,
    detail,
    action,
    children,
}: {
    icon: React.ReactNode;
    title: string;
    detail: React.ReactNode;
    action: React.ReactNode;
    children?: React.ReactNode;
}) {
    return (
        <div className="px-3 py-2.5 space-y-2">
            <div className="flex flex-wrap items-start gap-x-3 gap-y-2">
                <div className="flex items-start gap-2 min-w-0 flex-1">
                    <span className="mt-0.5 text-slate-400 shrink-0">{icon}</span>
                    <div className="min-w-0">
                        <div className="text-[12.5px] font-medium text-slate-900">{title}</div>
                        <div className="text-[11px] text-slate-500 leading-relaxed">{detail}</div>
                    </div>
                </div>
                <div className="shrink-0 ml-auto">{action}</div>
            </div>
            {children}
        </div>
    );
}

const primary = "h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors";
const secondary = "h-7 px-2.5 rounded-md border text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors";

function Expand({ open, children }: { open: boolean; children: React.ReactNode }) {
    return (
        <AnimatePresence initial={false}>
            {open && (
                <motion.div
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={{ opacity: 0, height: 0 }}
                    transition={{ duration: 0.18 }}
                    className="overflow-hidden"
                >
                    {children}
                </motion.div>
            )}
        </AnimatePresence>
    );
}

export default function MailboxSigninSection({ mailbox }: { mailbox: Mailbox }) {
    const q = useMailboxSignin(mailbox.id);
    const vendorStart = useStartVendorReconnect();
    const move = useMoveToGrant();
    const google = useGoogleReconnect();
    const [open, setOpen] = React.useState<"google" | "password" | null>(null);
    const confirm = useConfirm();
    const [passwordDirty, setPasswordDirty] = React.useState(false);
    // Leaving a typed app password asks first; the password is not kept anywhere.
    const toggle = (which: "google" | "password") => {
        const next = open === which ? null : which;
        if (open === "password" && passwordDirty) {
            confirm.show("Discard the app password you typed?", async () => setOpen(next));
            return;
        }
        setOpen(next);
    };
    const [importId, setImportId] = React.useState<string | null>(null);
    const s = q.data;
    // The dialog outlives the move, which changes what the section shows.
    const dialog = <MailboxImportDialog importId={importId} onClose={() => setImportId(null)} />;
    if (!s || s.method === "other") return dialog;

    const m = methodCopy(s);
    const o = s.options;
    const vendor = o.vendor ? vendorLabel(o.vendor.vendor) || o.vendor.label : "";
    const switching = reconnectOpen(s.switch?.status);
    const anything = !!(o.vendor || o.google_signin || o.grant || o.app_password);

    async function viaVendor() {
        if (!o.vendor || switching || vendorStart.isPending) return;
        try {
            await vendorStart.mutateAsync({ id: o.vendor.connection_id, emailAccountIds: [mailbox.id] });
            toast.success(`${vendor} is reconnecting ${mailbox.email}`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    async function moveIt() {
        if (!o.grant || move.isPending) return;
        try {
            const out = await move.mutateAsync({ grantId: o.grant.id, emails: [mailbox.email] });
            toast.success(`Moving ${mailbox.email} onto the admin grant`);
            setImportId(out.job.id);
        } catch (e) {
            toast.error(e instanceof MoveError ? e.message : buildError(e as AppError));
        }
    }

    const reconnecting = google.busy === mailbox.id;

    return (
        <div className="px-5 py-5 space-y-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Sign-in</div>
            <div className="flex items-start gap-2.5">
                <ProviderLogo id="google" size="sm" className="mt-px" />
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                        <span className="text-[12.5px] font-medium text-slate-900">{m.title}</span>
                        {m.tone === "ok" && (
                            <span className="h-[18px] px-1.5 rounded-full border border-emerald-200 bg-emerald-50 text-emerald-700 text-[10.5px] font-medium inline-flex items-center">
                                Full mail access
                            </span>
                        )}
                    </div>
                    <p className="text-[11px] text-slate-500 leading-relaxed">{m.detail}</p>
                </div>
            </div>

            <SwitchStatus s={s} />

            {s.managed && <p className="text-[11.5px] text-slate-500">Warmbly Cloud holds this mailbox&apos;s sign-in, so it is changed there.</p>}

            {s.auto_switch && !s.switch && (
                <p className="text-[11.5px] text-slate-600 leading-relaxed flex items-start gap-1.5">
                    <SparklesIcon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0" />
                    {vendor} reconnects it with full mail access on its own, soon. Nothing to do here.
                </p>
            )}

            {anything && (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    {o.vendor && (
                        <OptionRow
                            icon={<RefreshCwIcon className="w-3.5 h-3.5" />}
                            title={`Reconnect through ${vendor}`}
                            detail={`${vendor} adds Warmbly as a trusted app on the domain and signs in as the mailbox for you. Nothing to click at Google.`}
                            action={
                                <button type="button" onClick={() => void viaVendor()} aria-disabled={switching || vendorStart.isPending} className={cn(primary, (switching || vendorStart.isPending) && "opacity-60")}>
                                    {switching || vendorStart.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCwIcon className="w-3 h-3" />}
                                    {switching ? "Reconnecting" : "Reconnect"}
                                </button>
                            }
                        />
                    )}
                    {o.google_signin && (
                        <OptionRow
                            icon={<ShieldCheckIcon className="w-3.5 h-3.5" />}
                            title="Reconnect with Google"
                            detail="Sign in again with full mail access, once your Workspace admin has marked Warmbly trusted for the domain."
                            action={
                                <button
                                    type="button"
                                    onClick={() => toggle("google")}
                                    aria-expanded={open === "google"}
                                    className={cn(secondary, open === "google" ? "border-sky-200 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50")}
                                >
                                    Set up
                                    <ChevronDownIcon className={cn("w-3 h-3 transition-transform", open === "google" && "rotate-180")} />
                                </button>
                            }
                        >
                            <Expand open={open === "google"}>
                                <div className="space-y-2.5 pt-1">
                                    <p className="text-[12px] font-medium text-slate-900">First, your Workspace admin trusts Warmbly, once per domain</p>
                                    <TrustedAppSteps clientId={o.google_signin.client_id} />
                                    <button type="button" onClick={() => void google.reconnect(mailbox.id)} aria-disabled={!!google.busy} className={cn(primary, google.busy && "opacity-60")}>
                                        {reconnecting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCwIcon className="w-3 h-3" />}
                                        {reconnecting ? "Waiting for authorization…" : "Reconnect with Google"}
                                    </button>
                                    {google.unfinished === mailbox.id && <BlockedByAdminHint clientId={o.google_signin.client_id} />}
                                </div>
                            </Expand>
                        </OptionRow>
                    )}
                    {o.grant && (
                        <OptionRow
                            icon={<ArrowRightIcon className="w-3.5 h-3.5" />}
                            title={`Move onto the admin grant for ${o.grant.domain}`}
                            detail="The domain already has an active grant, so the mailbox can connect through it."
                            action={
                                <button type="button" onClick={() => void moveIt()} aria-disabled={move.isPending} className={cn(secondary, "border-slate-200 bg-white text-slate-700 hover:bg-slate-50", move.isPending && "opacity-60")}>
                                    {move.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                    Move
                                </button>
                            }
                        />
                    )}
                    {o.app_password && (
                        <OptionRow
                            icon={<KeyRoundIcon className="w-3.5 h-3.5" />}
                            title="Switch to an app password"
                            detail="Gmail checks the password first; if it is refused, nothing changes."
                            action={
                                <button
                                    type="button"
                                    onClick={() => toggle("password")}
                                    aria-expanded={open === "password"}
                                    className={cn(secondary, open === "password" ? "border-sky-200 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50")}
                                >
                                    Set up
                                    <ChevronDownIcon className={cn("w-3 h-3 transition-transform", open === "password" && "rotate-180")} />
                                </button>
                            }
                        >
                            <Expand open={open === "password"}>
                                <AppPasswordSwitchForm mailbox={mailbox} onDirtyChange={setPasswordDirty} onDone={() => setOpen(null)} />
                            </Expand>
                        </OptionRow>
                    )}
                </div>
            )}
            {dialog}
        </div>
    );
}

/** The Overview tab's pointer to the Sign-in section, shown only when there is something to see or do. */
export function SigninPrompt({ mailbox, onOpen }: { mailbox: Mailbox; onOpen: () => void }) {
    const s = useMailboxSignin(mailbox.id).data;
    if (!s || s.managed) return null;
    const sw = s.switch;
    const older = (s.method === "google_signin" && !s.full_mail_access) || s.method === "google_grant";
    const live = reconnectOpen(sw?.status) || sw?.status === "failed";
    if (!live && !(older && (s.auto_switch || s.options.vendor || s.options.google_signin || s.options.app_password))) return null;
    const vendor = vendorLabel(s.options.vendor?.vendor) || "The vendor";
    let text = "This mailbox uses the older Google sign-in. It keeps working, and you can change how it signs in without losing anything.";
    if (s.method === "google_grant") text = "This mailbox connects through a legacy domain grant. It keeps working, and you can change how it signs in without losing anything.";
    if (s.auto_switch && !sw) text = `${vendor} reconnects this mailbox with full mail access on its own.`;
    if (reconnectOpen(sw?.status)) text = `${vendor} ${sw ? STATUS_COPY[sw.status] : ""}.`;
    if (sw?.status === "failed") text = `${vendor} could not reconnect this mailbox. It keeps working on its current sign-in.`;
    return (
        <div className="px-5 py-3 flex items-start gap-2.5">
            <ProviderLogo id="google" size="sm" className="mt-px" />
            <p className="min-w-0 flex-1 text-[12px] text-slate-600 leading-relaxed">
                {text}{" "}
                <button type="button" onClick={onOpen} className="text-sky-700 underline decoration-sky-300 hover:decoration-sky-600 font-medium">
                    Sign-in options
                </button>
            </p>
        </div>
    );
}
