// Connect-mailbox modal — themed, single column, three providers.
//
// Same chrome as every other dialog in the app: 48px header band with
// eyebrow + subtitle + close, hairline rows, slate-900 primary,
// 28px buttons. Replaces the old indigo-gradient "feature billboard"
// triptych.
//
// Flow:
//   provider picker ─► Google ─► Workspace: existing delegation grants only ─► /emails/grants (GrantImportWizard)
//                    │         ├► personal Gmail: app password walkthrough ─► /emails/onboarding/smtp-imap
//                    │         └► Workspace: Sign in with Google ─┐ (trusted app; when frontend and backend allow it)
//                    ├► Microsoft ─► whole organization ─► /emails/grants (GrantImportWizard)
//                    │            └► sign in one mailbox ─────────┴► /emails/onboarding/oauth/finish
//                    ├► smtp/imap form ──────────► /emails/onboarding/smtp-imap
//                    ├► mailbox import ──────────► /emails/imports (MailboxImportWizard)
//                    └► inbox vendor ────────────► /emails/vendors (VendorImportWizard)
//
// Google and Microsoft each open a method chooser. Google splits Workspace
// (sign in each mailbox once the admin trusts the app; delegation only for
// grants already held) from personal Gmail (app password). Per-mailbox Google sign-in requires the
// frontend's opt-in and either a confirmed backend capability or a Warmbly
// Cloud connection; a personal Gmail address is always sent to app passwords.
//
// Every path can run into the workspace's mailbox allowance; that answer
// (code mailbox_allowance_reached) opens MailboxAllowanceDialog instead of a
// toast, and the picker shows the allowance up front so it is never a surprise.
//
// The OAuth popup (direct or through Warmbly Cloud) lives in useMailboxOAuth,
// shared with the import wizard's per-row "Sign in".

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    Building2Icon,
    CheckIcon,
    ChevronRightIcon,
    FileSpreadsheetIcon,
    InboxIcon,
    KeyRoundIcon,
    Loader2Icon,
    LogInIcon,
    MailIcon,
    ExternalLinkIcon,
    SendIcon,
    SettingsIcon,
    ShieldCheckIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";

import { Logo } from "@/components/svg";
import { TextInput } from "@/components/ui/field";
import { useUserProfile } from "@/hooks/context/user";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import addEmail from "@/lib/api/client/app/emails/addEmail";
import {
    allowsNoEncryption,
    defaultImapSecurity,
    defaultSmtpSecurity,
    offersCertificateTrust,
    validPort,
    type MailSecurity,
} from "@/lib/api/models/app/emails/Service";
import SecuritySelect from "@/components/app/emails/SecuritySelect";
import CertificateTrustOption from "@/components/app/emails/CertificateTrustOption";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import CloudMailboxPicker from "@/components/app/emails/CloudMailboxPicker";
import { CLOUD_MAILBOX_PICKER_EVENT } from "@/lib/cloudMailboxConnect";
import useMailboxOAuth, { isAllowanceError, type MailboxOAuthProvider } from "@/hooks/useMailboxOAuth";
import { useConfirm } from "@/hooks/context/confirm";
import { Google, Outlook } from "@/components/svg";
import { cn } from "@/lib/utils";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useMailboxAllowance from "@/lib/api/hooks/app/emails/useMailboxAllowance";
import { allowanceFull } from "@/lib/api/models/app/emails/MailboxAllowance";
import type MailboxAllowance from "@/lib/api/models/app/emails/MailboxAllowance";
import MailboxAllowanceDialog from "@/components/app/emails/MailboxAllowanceDialog";
import GmailAppPasswordPanel from "@/components/app/emails/GmailAppPasswordPanel";
import GoogleWorkspaceSigninPanel from "@/components/app/emails/GoogleWorkspaceSigninPanel";
import MailboxImportWizard from "@/components/app/emails/import/MailboxImportWizard";
import VendorImportWizard from "@/components/app/emails/import/vendors/VendorImportWizard";
import GrantImportWizard from "@/components/app/emails/import/grants/GrantImportWizard";
import ProviderLogo, { LogoStack } from "@/components/app/emails/ProviderLogo";
import { useGrantConfig, useGrants } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { useMailboxSourceBusy } from "@/lib/api/hooks/app/emails/mailboxSourceBusy";
import { useVendorCatalog } from "@/lib/api/hooks/app/emails/useMailboxVendors";
import { DitherMeter, type DitherTone } from "@/components/ui/dither";
import { SkeletonCards } from "@/components/app/emails/import/Discovering";
import { CopyValue } from "@/components/app/emails/import/parts";
import { Checkbox } from "@/components/ui/checkbox";

import HelpLink from "@/components/shared/HelpLink";
type View =
    | "pick"
    | "google"
    | "microsoft"
    | "gmail"
    | "gmail_app_password"
    | "outlook"
    | "smtp_imap"
    | "bulk"
    | "vendor"
    | "grant_google"
    | "grant_microsoft";

// The multi-step import views: wider, and guarded while they hold a draft.
const WIZARD_VIEWS: View[] = ["bulk", "vendor", "grant_google", "grant_microsoft"];

// Where Back goes: a method returns to its provider's chooser.
const PARENT: Record<View, View> = {
    pick: "pick",
    google: "pick",
    microsoft: "pick",
    gmail: "google",
    gmail_app_password: "google",
    grant_google: "google",
    outlook: "microsoft",
    grant_microsoft: "microsoft",
    smtp_imap: "pick",
    bulk: "pick",
    vendor: "pick",
};

function depth(v: View): number {
    return v === "pick" ? 0 : PARENT[v] === "pick" ? 1 : 2;
}

type OAuthProvider = MailboxOAuthProvider;

export default function AddEmailModal() {
    const user = useUserProfile();
    const qc = useQueryClient();

    const [view, setView] = React.useState<View>("pick");
    const [cloudAddress, setCloudAddress] = React.useState("");
    // A personal Gmail address typed on the sign-in panel, carried into the app-password walkthrough.
    const [appPasswordEmail, setAppPasswordEmail] = React.useState("");
    // The CSV import is reached from the picker and from the Google chooser; Back returns to whichever opened it.
    const [bulkFrom, setBulkFrom] = React.useState<View>("pick");
    // Deeper views slide in from the right, shallower ones from the left.
    const [dir, setDir] = React.useState<1 | -1>(1);
    const navigate = React.useCallback(
        (v: View) => {
            if (v === "bulk" && view !== "bulk") setBulkFrom(view);
            setDir(view === "bulk" && v === bulkFrom ? -1 : depth(v) >= depth(view) ? 1 : -1);
            setView(v);
        },
        [view, bulkFrom],
    );
    // Set when the deployment has no OAuth client for the provider the user
    // picked. Rendered inline rather than as a toast: it is a setup instruction
    // with a link, not a transient failure.
    const [notConfigured, setNotConfigured] = React.useState<OAuthProvider | null>(null);
    // An import wizard holds a file, a list or a pick nobody has imported yet.
    const [wizardDirty, setWizardDirty] = React.useState(false);
    const confirm = useConfirm();

    // The allowance is read while the modal is open so the picker can show it
    // and a refused connect can explain itself. Fetched from the same query
    // the mailbox list invalidates, so it is live.
    const access = useFeatureAccess();
    const allowance = useMailboxAllowance(user.addEmail);
    const [allowanceOpen, setAllowanceOpen] = React.useState(false);
    const [allowanceReached, setAllowanceReached] = React.useState(false);
    const openAllowance = React.useCallback((reached = false) => {
        setAllowanceReached(reached);
        setAllowanceOpen(true);
    }, []);
    // Route a refused connect to the dialog; everything else stays a toast.
    const onConnectError = React.useCallback(
        (e: unknown) => {
            if (isAllowanceError(e)) openAllowance(true);
        },
        [openAllowance],
    );

    const oauth = useMailboxOAuth({
        onConnected: () => user.setAddEmail(false),
        onAllowance: () => openAllowance(true),
        onNotConfigured: setNotConfigured,
        onPersonalGmail: () => navigate("gmail_app_password"),
    });
    const grantConfig = useGrantConfig(user.addEmail);
    const oauthBusy = oauth.busy;
    const viaCloud = oauth.viaCloud;
    const gmailOAuth = oauth.gmailAvailable;
    const resetOAuth = oauth.reset;

    // Reset when the modal closes.
    React.useEffect(() => {
        if (!user.addEmail) {
            setCloudAddress("");
            setAppPasswordEmail("");
            setView("pick");
            setDir(1);
            setNotConfigured(null);
            setAllowanceOpen(false);
            setWizardDirty(false);
            resetOAuth();
        }
    }, [user.addEmail, resetOAuth]);

    const finishWizard = () => {
        qc.invalidateQueries({ queryKey: ["emails", "list"] });
        setWizardDirty(false);
        user.setAddEmail(false);
    };

    function startOAuth(provider: OAuthProvider, loginHint?: string) {
        setNotConfigured(null);
        void oauth.start(provider, { loginHint });
    }

    // Every way out of the modal (close, backdrop, Escape, back to the picker)
    // asks first while the import wizard holds work nobody has imported yet.
    // An import or grant request in flight finishes before the modal can be left.
    const sourceBusy = useMailboxSourceBusy() && user.addEmail;
    const guard = React.useCallback(
        (leave: () => void) => {
            if (sourceBusy) return;
            if (WIZARD_VIEWS.includes(view) && wizardDirty) {
                const text =
                    view === "bulk"
                        ? "Discard this import? The file and the choices made for it are lost."
                        : "Discard this import? What you picked and entered here is lost.";
                confirm.show(text, async () => {
                    setWizardDirty(false);
                    leave();
                });
                return;
            }
            leave();
        },
        [view, wizardDirty, confirm, sourceBusy],
    );
    const requestClose = React.useCallback(() => guard(() => user.setAddEmail(false)), [guard, user]);

    React.useEffect(() => {
        const openCloudMailbox = (event: Event) => guard(() => {
            setCloudAddress((event as CustomEvent<{ email: string }>).detail.email);
            setView("pick");
            user.setAddEmail(true);
        });
        window.addEventListener(CLOUD_MAILBOX_PICKER_EVENT, openCloudMailbox);
        return () => window.removeEventListener(CLOUD_MAILBOX_PICKER_EVENT, openCloudMailbox);
    }, [guard, user]);

    React.useEffect(() => {
        if (!user.addEmail) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // An open dropdown, the allowance dialog or the discard confirm owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']") || allowanceOpen) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [user.addEmail, requestClose, allowanceOpen]);

    return (
        <AnimatePresence>
            {user.addEmail && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={requestClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className={cn(
                            "w-full rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh] transition-[max-width] duration-200",
                            WIZARD_VIEWS.includes(view) ? "max-w-[760px]" : "max-w-[560px]",
                        )}
                    >
                        <Header
                            view={view}
                            onBack={() =>
                                guard(() => {
                                    setNotConfigured(null);
                                    setWizardDirty(false);
                                    navigate(view === "bulk" ? bulkFrom : PARENT[view]);
                                })
                            }
                            onClose={requestClose}
                            busy={sourceBusy}
                        />
                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden relative">
                            <AnimatePresence mode="wait" initial={false} custom={dir}>
                                <motion.div
                                    key={view}
                                    custom={dir}
                                    variants={SLIDE}
                                    initial="enter"
                                    animate="center"
                                    exit="exit"
                                    transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
                                >
                                    {view === "pick" && (
                                        <>
                                            <AllowanceStrip
                                                allowance={allowance.data}
                                                onOpen={() => openAllowance(allowanceFull(allowance.data))}
                                            />
                                            <PickProvider
                                                onPick={navigate}
                                                viaCloud={viaCloud}
                                                open={user.addEmail}
                                                cloudAddress={cloudAddress}
                                                busy={sourceBusy}
                                            />
                                        </>
                                    )}
                                    {(view === "google" || view === "microsoft") && (
                                        <MethodChooser
                                            provider={view}
                                            onPick={navigate}
                                            viaCloud={viaCloud}
                                            gmailOAuth={gmailOAuth}
                                            open={user.addEmail}
                                        />
                                    )}
                                    {view === "gmail" && (
                                        notConfigured === "gmail" ? (
                                            <ProviderNotConfigured provider="gmail" />
                                        ) : (
                                            <GoogleWorkspaceSigninPanel
                                                busy={oauthBusy === "gmail"}
                                                viaCloud={viaCloud}
                                                clientId={grantConfig.data?.google_mailbox_client_id}
                                                unfinished={oauth.unfinished === "gmail"}
                                                onConnect={(address) => {
                                                    setAppPasswordEmail(address);
                                                    startOAuth("gmail", address);
                                                }}
                                                onPersonalGmail={(address) => {
                                                    setAppPasswordEmail(address);
                                                    navigate("gmail_app_password");
                                                }}
                                                onUseAppPassword={(address) => {
                                                    setAppPasswordEmail(address);
                                                    navigate("gmail_app_password");
                                                }}
                                            />
                                        )
                                    )}
                                    {view === "gmail_app_password" && (
                                        <GmailAppPasswordPanel
                                            initialEmail={appPasswordEmail}
                                            onDone={() => {
                                                qc.invalidateQueries({ queryKey: ["emails", "list"] });
                                                user.setAddEmail(false);
                                            }}
                                            onError={onConnectError}
                                        />
                                    )}
                                    {view === "outlook" && (
                                        notConfigured === "outlook" ? (
                                            <ProviderNotConfigured provider="outlook" />
                                        ) : (
                                            <OAuthPanel
                                                provider="outlook"
                                                busy={oauthBusy === "outlook"}
                                                viaCloud={viaCloud}
                                                adminApprovalUrl={oauth.adminConsentUrl}
                                                onConnect={() => startOAuth("outlook")}
                                            />
                                        )
                                    )}
                                    {view === "smtp_imap" && (
                                        <SmtpImapPanel
                                            onDone={() => {
                                                qc.invalidateQueries({ queryKey: ["emails", "list"] });
                                                user.setAddEmail(false);
                                            }}
                                            onError={onConnectError}
                                        />
                                    )}
                                    {view === "bulk" && (
                                        <MailboxImportWizard
                                            onDone={finishWizard}
                                            onAllowance={() => openAllowance(allowanceFull(allowance.data))}
                                            onDirtyChange={setWizardDirty}
                                        />
                                    )}
                                    {view === "vendor" && (
                                        <VendorImportWizard
                                            onDone={finishWizard}
                                            onAllowance={() => openAllowance(allowanceFull(allowance.data))}
                                            onDirtyChange={setWizardDirty}
                                        />
                                    )}
                                    {(view === "grant_google" || view === "grant_microsoft") && (
                                        <GrantImportWizard
                                            key={view}
                                            provider={view === "grant_google" ? "google" : "microsoft"}
                                            onDone={finishWizard}
                                            onAllowance={() => openAllowance(allowanceFull(allowance.data))}
                                            onDirtyChange={setWizardDirty}
                                        />
                                    )}
                                </motion.div>
                            </AnimatePresence>
                        </div>
                    </motion.div>
                    <MailboxAllowanceDialog
                        open={allowanceOpen}
                        onClose={() => setAllowanceOpen(false)}
                        allowance={allowance.data}
                        reached={allowanceReached}
                        currentPlan={access.plan}
                    />
                </motion.div>
            )}
        </AnimatePresence>
    );
}

// AllowanceStrip — the workspace's mailbox allowance at the top of the
// picker: quiet while there is plenty of room, a warning near the cap, and a
// clear "full" state that leads to the request dialog rather than letting the
// user type credentials that will be refused.
function AllowanceStrip({ allowance: a, onOpen }: { allowance: MailboxAllowance | undefined; onOpen: () => void }) {
    if (!a || a.allowance == null) return null;
    const cap = a.allowance;
    const remaining = a.remaining ?? 0;
    const pct = Math.min(100, Math.round((a.used / cap) * 100));
    const full = remaining <= 0;
    const near = !full && (pct >= 80 || remaining <= 5);
    if (!full && !near) {
        return (
            <div className="px-4 py-2 border-b border-slate-200/60 flex items-center gap-2 text-[11px] text-slate-500">
                <span className="font-mono tabular-nums text-slate-700">
                    {a.used.toLocaleString()} / {cap.toLocaleString()}
                </span>
                <span>mailboxes on this workspace</span>
                {a.pending_request && <span className="text-amber-600">· increase requested</span>}
                <button type="button" onClick={onOpen} className="ml-auto underline hover:text-slate-900 transition-colors">
                    Need more?
                </button>
            </div>
        );
    }
    const tone: DitherTone = full ? "rose" : "amber";
    return (
        <div className={cn("px-4 py-2.5 border-b", full ? "bg-rose-50/60 border-rose-200/60" : "bg-amber-50/60 border-amber-200/60")}>
            <div className="flex items-baseline justify-between gap-2 mb-1">
                <span className={cn("text-[12px] font-medium", full ? "text-rose-900" : "text-amber-900")}>
                    {full
                        ? "Every mailbox slot is used"
                        : `${remaining.toLocaleString()} ${remaining === 1 ? "slot" : "slots"} left`}
                </span>
                <span className="text-[11px] font-mono tabular-nums text-slate-700">
                    {a.used.toLocaleString()} / {cap.toLocaleString()}
                </span>
            </div>
            <DitherMeter frac={pct / 100} tone={tone} height={4} />
            <div className="flex items-center justify-between gap-2 mt-1.5">
                <span className={cn("text-[11px]", full ? "text-rose-800/90" : "text-amber-800/90")}>
                    {a.pending_request
                        ? `Increase to ${a.pending_request.requested.toLocaleString()} requested, pending review`
                        : full
                          ? "New connects are refused until the allowance is raised"
                          : "Ask for more before a large batch"}
                </span>
                <button
                    type="button"
                    onClick={onOpen}
                    className={cn(
                        "h-6 px-2 rounded text-[11px] font-medium transition-colors shrink-0",
                        full ? "bg-rose-600 hover:bg-rose-700 text-white" : "bg-amber-600 hover:bg-amber-700 text-white",
                    )}
                >
                    {a.pending_request ? "View request" : full ? "Get more mailboxes" : "Request more"}
                </button>
            </div>
        </div>
    );
}

function Header({
    view,
    onBack,
    onClose,
    busy = false,
}: {
    view: View;
    onBack: () => void;
    onClose: () => void;
    /** A request is in flight: back and close wait for it. */
    busy?: boolean;
}) {
    const sub: Record<View, string> = {
        pick: "Connect a sending account",
        google: "Gmail and Google Workspace",
        microsoft: "Outlook and Microsoft 365",
        gmail: "Sign in with Google",
        gmail_app_password: "Gmail app password",
        outlook: "Sign in one Microsoft mailbox",
        smtp_imap: "Any provider via SMTP / IMAP",
        bulk: "Import mailboxes",
        vendor: "Import from an inbox vendor",
        grant_google: "Workspace domain grant",
        grant_microsoft: "Whole Microsoft 365 organization",
    };
    return (
        <div className="h-12 px-3 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
            {view !== "pick" && (
                <button
                    type="button"
                    onClick={onBack}
                    disabled={busy}
                    aria-label="Back"
                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
                >
                    <ArrowLeftIcon className="w-3.5 h-3.5" />
                </button>
            )}
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                Mailbox
            </span>
            <div className="h-4 w-px bg-slate-200" />
            <span className="text-[12px] text-slate-600 truncate">{sub[view]}</span>
            <button
                type="button"
                onClick={onClose}
                disabled={busy}
                aria-label="Close"
                title={busy ? "Wait for this to finish" : undefined}
                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
            >
                <XIcon className="w-3.5 h-3.5" />
            </button>
        </div>
    );
}

// Shown when the API reports this deployment has no OAuth client for the chosen
// provider. Self-host only, and the person seeing it can usually fix it, so it
// names the exact variables and links the setup guide instead of just failing.
const PROVIDER_SETUP: Record<OAuthProvider, { label: string; vars: string[] }> = {
    gmail: {
        label: "Gmail and Google Workspace",
        vars: ["BOX_GOOGLE_CLIENT_ID", "BOX_GOOGLE_CLIENT_SECRET"],
    },
    outlook: {
        label: "Outlook and Microsoft 365",
        vars: ["BOX_OUTLOOK_CLIENT_ID", "BOX_OUTLOOK_CLIENT_SECRET"],
    },
};

function ProviderNotConfigured({ provider }: { provider: OAuthProvider }) {
    const { label, vars } = PROVIDER_SETUP[provider];
    return (
        <div className="p-4">
            <div className="rounded-md border border-amber-200 bg-amber-50 p-3">
                <div className="flex items-start gap-2.5">
                    <SettingsIcon className="w-4 h-4 text-amber-600 mt-0.5 shrink-0" />
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-amber-900">
                            {label} is not configured on this deployment
                        </p>
                        <p className="text-[12.5px] text-amber-800 mt-1">
                            Connecting these mailboxes needs an OAuth client. Add both values to
                            the <code className="bg-white/70 px-1 rounded">.env</code> at the root of
                            your Warmbly install, then restart with{" "}
                            <code className="bg-white/70 px-1 rounded">make up</code>.
                        </p>
                        <ul className="mt-2 space-y-1">
                            {vars.map((v) => (
                                <li
                                    key={v}
                                    className="text-[12px] font-mono text-amber-900 bg-white/70 rounded px-1.5 py-1"
                                >
                                    {v}=
                                </li>
                            ))}
                        </ul>
                    </div>
                </div>
            </div>

            <HelpLink
                href="https://docs.warmbly.com/development/deployment-guide/#connect-mailboxes"
                className="mt-3 h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 text-[12.5px] text-slate-700 hover:bg-slate-50 transition-colors"
                portalLabel={<><ExternalLinkIcon className="w-3.5 h-3.5" /> Contact support</>}
            >
                <ExternalLinkIcon className="w-3.5 h-3.5" />
                Full environment setup guide
            </HelpLink>

            <p className="mt-3 text-[12px] text-slate-500">
                No setup needed for any other provider: connect it over SMTP and IMAP instead.
            </p>
        </div>
    );
}

const SLIDE = {
    enter: (d: 1 | -1) => ({ opacity: 0, x: d * 16 }),
    center: { opacity: 1, x: 0 },
    exit: (d: 1 | -1) => ({ opacity: 0, x: d * -16 }),
};

function PickProvider({
    onPick,
    viaCloud,
    open,
    cloudAddress,
    busy,
}: {
    onPick: (v: View) => void;
    viaCloud: boolean;
    /** The modal is open, so the import sources may be asked what they offer. */
    open: boolean;
    cloudAddress: string;
    busy: boolean;
}) {
    // Asked now so the Google and Microsoft choosers open with their methods known.
    useGrantConfig(open);
    const vendorCatalog = useVendorCatalog(open);
    // A backend without the vendor service answers 404; anything else still offers the route.
    const vendorsOff = (vendorCatalog.error as AppError | null)?.status === 404;

    const rows: Array<{ key: View; icon: React.ReactNode; title: string; sub: string }> = [
        { key: "google", icon: <Google className="w-5 h-5" />, title: "Google", sub: "Gmail and Google Workspace" },
        { key: "microsoft", icon: <Outlook className="w-5 h-5" />, title: "Microsoft", sub: "Outlook and Microsoft 365" },
        {
            key: "smtp_imap",
            icon: <Logo className="w-4 h-5 text-slate-700" />,
            title: "Other (SMTP / IMAP)",
            sub: "Any provider with manual host, port, and app password.",
        },
        {
            key: "bulk",
            icon: <FileSpreadsheetIcon className="w-4 h-4 text-slate-700" />,
            title: "Import mailboxes",
            sub: "CSV, spreadsheet or pasted list. Server settings are detected for you.",
        },
        ...(vendorsOff
            ? []
            : [
                  {
                      key: "vendor" as View,
                      icon: <LogoStack ids={["inboxkit", "zapmail"]} size="xs" />,
                      title: "Inbox vendor",
                      sub: "InboxKit, Zapmail, Mailforge and more. Paste an API key, pick mailboxes.",
                  },
              ]),
    ];
    return (
        <div className="divide-y divide-slate-200/60">
            {viaCloud && <CloudMailboxPicker initialSearch={cloudAddress} />}
            {rows.map((r, i) => (
                <motion.button
                    key={r.key}
                    type="button"
                    disabled={busy}
                    onClick={() => onPick(r.key)}
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: 0.04 + i * 0.04, duration: 0.18, ease: "easeOut" }}
                    className="w-full px-4 py-3.5 flex items-center gap-3 text-left group hover:bg-slate-50 transition-colors"
                >
                    <div className="size-9 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0 transition-colors group-hover:border-slate-300">
                        {r.icon}
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="text-[13px] font-medium text-slate-900 truncate">{r.title}</div>
                        <div className="text-[11.5px] text-slate-500 truncate">{r.sub}</div>
                    </div>
                    <ChevronRightIcon className="w-4 h-4 text-slate-300 shrink-0 group-hover:text-slate-500 group-hover:translate-x-0.5 transition-all" />
                </motion.button>
            ))}
        </div>
    );
}

interface Method {
    view: View;
    icon: React.ReactNode;
    title: string;
    sub: string;
    pill?: { label: string; tone: "sky" | "amber" };
    /** The kind of account it is for; a heading is drawn where it changes. */
    section?: { title: string; sub: string };
}

const WORKSPACE_SECTION = { title: "Google Workspace", sub: "A mailbox on your company's own domain" };
const PERSONAL_GMAIL_SECTION = { title: "Personal Gmail", sub: "An @gmail.com or @googlemail.com account" };

// The ways to connect a Google or Microsoft mailbox. Google separates a
// Workspace domain from personal Gmail, which only connects with an app password.
function MethodChooser({
    provider,
    onPick,
    viaCloud,
    gmailOAuth,
    open,
}: {
    provider: "google" | "microsoft";
    onPick: (v: View) => void;
    viaCloud: boolean;
    /** Per-mailbox Google sign-in is still allowed on this deployment. */
    gmailOAuth: boolean;
    open: boolean;
}) {
    const grantConfig = useGrantConfig(open);
    // Every other grant is reached from View the grant in its mailboxes' drawers.
    const orphanedGoogleGrant = (useGrants(open && provider === "google").data?.data ?? []).some((g) => g.provider === "google" && g.mailboxes === 0);
    const busy = useMailboxSourceBusy();
    const methods: Method[] = [];
    if (provider === "google") {
        if (gmailOAuth) {
            methods.push({
                view: "gmail",
                icon: <ProviderLogo id="google" size="md" framed={false} />,
                title: "Sign in with Google",
                sub: viaCloud
                    ? "One mailbox at a time, through Warmbly Cloud."
                    : "Your Workspace admin marks Warmbly as a trusted app once, then each mailbox signs in with Google.",
                pill: { label: "Recommended", tone: "sky" },
                section: WORKSPACE_SECTION,
            });
        }
        // New delegation grants are retired; one left without a mailbox has no drawer to open it from.
        if (grantConfig.data?.google_enabled && orphanedGoogleGrant) {
            methods.push({
                view: "grant_google",
                icon: <Building2Icon className="w-4 h-4 text-slate-700" />,
                title: "Workspace domain grant",
                sub: "A domain grant you already hold, with no mailbox connected through it: connect its mailboxes again, check it or remove it.",
                section: WORKSPACE_SECTION,
            });
        }
        methods.push({
            view: "gmail_app_password",
            icon: <KeyRoundIcon className="w-4 h-4 text-slate-700" />,
            title: "App password",
            sub:
                methods.length > 0
                    ? "Guided: turn on 2-Step Verification, create a 16-letter app password, connect. Also works for one Workspace mailbox."
                    : "Guided, in about two minutes over IMAP and SMTP. The way in for personal Gmail, and it works for any Workspace mailbox too.",
            section: methods.length > 0 ? PERSONAL_GMAIL_SECTION : { title: "Personal Gmail or Workspace", sub: "Any Gmail or Workspace account" },
        });
    } else {
        if (grantConfig.data?.microsoft_enabled) {
            methods.push({
                view: "grant_microsoft",
                icon: <Building2Icon className="w-4 h-4 text-slate-700" />,
                title: "Whole organization",
                sub: "A Global Administrator approves Warmbly once, then you pick the mailboxes. No passwords.",
                pill: { label: "Recommended", tone: "sky" },
            });
        }
        methods.push({
            view: "outlook",
            icon: <LogInIcon className="w-4 h-4 text-slate-700" />,
            title: "Sign in one mailbox",
            sub: viaCloud
                ? "Through Warmbly Cloud. Warmup included, no OAuth app needed."
                : "Sign in with Microsoft, one mailbox at a time.",
        });
    }

    return (
        <div className="p-4 space-y-2">
            {viaCloud && <CloudMailboxPicker provider={provider === "google" ? "gmail" : "outlook"} />}
            <div className="flex items-center gap-2.5 pb-1">
                <ProviderLogo id={provider} size="lg" />
                <div className="min-w-0">
                    <p className="text-[13px] font-medium text-slate-900">How should Warmbly connect?</p>
                    <p className="text-[11.5px] text-slate-500">
                        {provider === "google" ? (gmailOAuth ? "Workspace mailboxes sign in with Google. Personal Gmail uses an app password." : "Personal @gmail.com? Use an app password.") : "Personal @outlook.com or @hotmail.com? Sign in one mailbox."}
                    </p>
                </div>
            </div>
            {grantConfig.isLoading && (
                <div role="status" aria-label="Checking what this instance supports">
                    <SkeletonCards count={1} />
                </div>
            )}
            {methods.map((m, i) => (
                <React.Fragment key={m.view}>
                    {m.section && m.section.title !== methods[i - 1]?.section?.title && (
                        <div className="pt-2 flex items-baseline gap-2 min-w-0">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">{m.section.title}</span>
                            <span className="text-[11px] text-slate-400 truncate">{m.section.sub}</span>
                        </div>
                    )}
                    <motion.button
                        type="button"
                        disabled={busy}
                        onClick={() => onPick(m.view)}
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: 0.04 + i * 0.05, duration: 0.18, ease: "easeOut" }}
                        className="w-full rounded-md border px-3 py-3 flex items-start gap-3 text-left group transition-colors border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50"
                    >
                        <div
                            className="size-8 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0"
                        >
                            {m.icon}
                        </div>
                        <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-1.5 min-w-0 flex-wrap">
                                <span className="text-[13px] font-medium text-slate-900">{m.title}</span>
                                {m.pill && (
                                    <span
                                        className={cn(
                                            "shrink-0 h-[18px] px-1.5 rounded-full border text-[10px] font-medium inline-flex items-center",
                                            m.pill.tone === "sky" ? "bg-sky-50 border-sky-200 text-sky-700" : "bg-amber-50 border-amber-200 text-amber-700",
                                        )}
                                    >
                                        {m.pill.label}
                                    </span>
                                )}
                            </div>
                            <p className="text-[11.5px] text-slate-500 leading-relaxed">{m.sub}</p>
                        </div>
                        <ChevronRightIcon className="w-4 h-4 text-slate-300 shrink-0 self-center group-hover:text-slate-500 group-hover:translate-x-0.5 transition-all" />
                    </motion.button>
                </React.Fragment>
            ))}
            {provider === "google" && (
                <p className="pt-1 text-[11.5px] text-slate-500">
                    Have a list of app passwords?{" "}
                    <button
                        type="button"
                        onClick={() => onPick("bulk")}
                        className="text-sky-700 underline decoration-sky-300 hover:decoration-sky-600 transition-colors"
                    >
                        Import them from a CSV
                    </button>
                </p>
            )}
        </div>
    );
}

function OAuthPanel({
    provider,
    busy,
    viaCloud,
    onConnect,
    adminApprovalUrl,
}: {
    provider: OAuthProvider;
    busy: boolean;
    viaCloud: boolean;
    onConnect: () => void;
    /** Microsoft only, after a first try: the approval page to forward to an administrator. */
    adminApprovalUrl?: string | null;
}) {
    const label = provider === "gmail" ? "Google" : "Microsoft";
    const Icon = provider === "gmail" ? Google : Outlook;
    return (
        <div className="px-5 py-6 space-y-5">
            <div className="flex items-center gap-3">
                <div className="size-11 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0">
                    <Icon className="w-6 h-6" />
                </div>
                <div>
                    <div className="text-[13.5px] font-medium text-slate-900">
                        Connect with {label}
                    </div>
                    <div className="text-[11.5px] text-slate-500">
                        {viaCloud
                            ? `Warmbly Cloud opens the ${label} window on its own app. Approve and you're done.`
                            : `We'll open a ${label} window. Approve the scopes and you're done.`}
                    </div>
                </div>
            </div>

            {viaCloud ? (
                <ul className="text-[11.5px] text-slate-600 space-y-1.5 px-1">
                    <Scope>Sends campaigns and syncs replies from this server, as usual</Scope>
                    <Scope>Warmbly Cloud keeps the sign-in and warms the mailbox in its pool</Scope>
                    <Scope>The mailbox also appears in your cloud workspace; remove it from either side</Scope>
                </ul>
            ) : (
                <ul className="text-[11.5px] text-slate-600 space-y-1.5 px-1">
                    <Scope>Send and read mail on your behalf</Scope>
                    <Scope>Track replies and deliveries</Scope>
                    <Scope>Refresh tokens are stored encrypted; revoke any time</Scope>
                </ul>
            )}

            <motion.button
                type="button"
                onClick={onConnect}
                disabled={busy}
                whileTap={busy ? undefined : { scale: 0.985 }}
                className="w-full h-9 rounded-md text-[12.5px] font-medium inline-flex items-center justify-center gap-2 transition-colors disabled:opacity-60 bg-slate-900 hover:bg-slate-800 text-white"
            >
                {busy ? (
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                ) : (
                    <ShieldCheckIcon className="w-3.5 h-3.5" />
                )}
                {busy ? "Waiting for authorization…" : `Continue with ${label}`}
            </motion.button>

            {adminApprovalUrl && !viaCloud && (
                <motion.div
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.18, ease: "easeOut" }}
                    className="rounded-md border border-slate-200 bg-white px-3 py-2.5 space-y-2"
                >
                    <div className="text-[12px] font-medium text-slate-900">Microsoft says &ldquo;Need admin approval&rdquo;?</div>
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        Your organization lets only an administrator approve new apps. Send them this link. Once they approve, every
                        mailbox in the organization can sign in here without them.
                    </p>
                    <CopyValue label="Approval link for your administrator" value={adminApprovalUrl} display="login.microsoftonline.com/organizations/v2.0/adminconsent" />
                </motion.div>
            )}
        </div>
    );
}

function Scope({ children }: { children: React.ReactNode }) {
    return (
        <li className="flex items-start gap-2">
            <CheckIcon className="w-3 h-3 text-slate-400 mt-1 shrink-0" />
            <span>{children}</span>
        </li>
    );
}

function SmtpImapPanel({ onDone, onError }: { onDone: () => void; onError: (e: unknown) => void }) {
    const [name, setName] = React.useState("");
    const [email, setEmail] = React.useState("");

    const [imapHost, setImapHost] = React.useState("");
    const [imapPort, setImapPort] = React.useState("993");
    const [imapUser, setImapUser] = React.useState("");
    const [imapPass, setImapPass] = React.useState("");
    const [imapSecurity, setImapSecurity] = React.useState<MailSecurity>("tls");

    const [smtpHost, setSmtpHost] = React.useState("");
    const [smtpPort, setSmtpPort] = React.useState("587");
    const [smtpUser, setSmtpUser] = React.useState("");
    const [smtpPass, setSmtpPass] = React.useState("");
    const [smtpSecurity, setSmtpSecurity] = React.useState<MailSecurity>("starttls");

    // The port implies the security mode for every conventional setup, so
    // typing a port moves the selector with it. Once the user picks a mode by
    // hand we stop guessing: that is exactly the non-standard case they came
    // here for (a submission relay on 2525, IMAP on a custom port).
    const imapSecurityTouched = React.useRef(false);
    const smtpSecurityTouched = React.useRef(false);
    React.useEffect(() => {
        if (!imapSecurityTouched.current) {
            setImapSecurity(defaultImapSecurity(Number(imapPort)));
        }
    }, [imapPort]);
    React.useEffect(() => {
        if (!smtpSecurityTouched.current) {
            setSmtpSecurity(defaultSmtpSecurity(Number(smtpPort)));
        }
    }, [smtpPort]);

    // "No encryption" is only offered for a local relay on a self-hosted
    // instance. Editing the host away from loopback has to take the mode with
    // it, or the form keeps a value the backend will reject and the user is
    // left reading an error about a control that is no longer on screen.
    const selfHosted = useAuthConfig().data?.self_hosted === true;
    React.useEffect(() => {
        if (imapSecurity === "none" && !allowsNoEncryption(imapHost, selfHosted)) {
            setImapSecurity(defaultImapSecurity(Number(imapPort)));
        }
    }, [imapHost, imapPort, imapSecurity, selfHosted]);
    React.useEffect(() => {
        if (smtpSecurity === "none" && !allowsNoEncryption(smtpHost, selfHosted)) {
            setSmtpSecurity(defaultSmtpSecurity(Number(smtpPort)));
        }
    }, [smtpHost, smtpPort, smtpSecurity, selfHosted]);

    // Single-credentials toggle — covers the 90% case where IMAP and SMTP
    // share the same login. The user can flip it off and supply distinct
    // SMTP creds for legacy setups.
    const [sameCreds, setSameCreds] = React.useState(true);
    const [submitting, setSubmitting] = React.useState(false);
    // Offered only after a test failed on the server's certificate.
    const [certFailed, setCertFailed] = React.useState(false);
    const [trustCert, setTrustCert] = React.useState(false);

    // Auto-fill the username fields from the email address so the user
    // doesn't have to re-type it. Cleared if they touched the user field.
    const imapUserTouched = React.useRef(false);
    const smtpUserTouched = React.useRef(false);
    React.useEffect(() => {
        if (!imapUserTouched.current) setImapUser(email);
        if (!smtpUserTouched.current && !sameCreds) setSmtpUser(email);
    }, [email, sameCreds]);

    function effectiveSmtp() {
        return sameCreds
            ? { user: imapUser, pass: imapPass, host: smtpHost, port: Number(smtpPort) }
            : { user: smtpUser, pass: smtpPass, host: smtpHost, port: Number(smtpPort) };
    }

    function valid() {
        if (!name.trim() || !email.trim()) return false;
        if (!imapHost.trim() || !imapPort.trim() || !imapUser.trim() || !imapPass) return false;
        if (!smtpHost.trim() || !smtpPort.trim()) return false;
        if (!sameCreds && (!smtpUser.trim() || !smtpPass)) return false;
        // Any routable port is allowed; the security mode carries how to
        // connect, so 2525 and other non-standard ports work.
        if (!validPort(Number(smtpPort)) || !validPort(Number(imapPort))) return false;
        return true;
    }

    async function submit() {
        if (submitting || !valid()) return;
        setSubmitting(true);
        const eff = effectiveSmtp();
        // The backend keeps the opt-out only on a server whose certificate needs it.
        const tls = trustCert ? { tls_unverified: true } : {};
        try {
            await toast.promise(
                addEmail({
                    name: name.trim(),
                    email: email.trim(),
                    imap: {
                        username: imapUser.trim(),
                        password: imapPass,
                        host: imapHost.trim(),
                        port: Number(imapPort),
                        security: imapSecurity,
                        ...tls,
                    },
                    smtp: {
                        username: eff.user.trim(),
                        password: eff.pass,
                        host: eff.host.trim(),
                        port: eff.port,
                        security: smtpSecurity,
                        ...tls,
                    },
                }),
                {
                    loading: "Verifying credentials…",
                    success: "Mailbox connected",
                    error: (e: AppError) => buildError(e),
                },
            );
            onDone();
        } catch (e) {
            // Surfaced by the toast, except a full allowance, which gets its dialog.
            if (offersCertificateTrust(e)) setCertFailed(true);
            onError(e);
        } finally {
            setSubmitting(false);
        }
    }

    return (
        <div>
            <Section title="Account" sub="Name and address you send from" icon={<MailIcon className="w-3.5 h-3.5" />}>
                <Field label="Name">
                    <TextInput value={name} onChange={setName} placeholder="Alex Rivera" />
                </Field>
                <Field label="Email">
                    <TextInput value={email} onChange={setEmail} placeholder="alex@company.com" />
                </Field>
            </Section>

            <Section title="IMAP" sub="Incoming, usually 993" icon={<InboxIcon className="w-3.5 h-3.5" />}>
                <Field label="Server">
                    <HostPortInput
                        host={imapHost}
                        onHost={setImapHost}
                        hostPlaceholder="imap.example.com"
                        port={imapPort}
                        onPort={setImapPort}
                        portPlaceholder="993"
                    />
                </Field>
                <Field label="Security">
                    <SecuritySelect
                        value={imapSecurity}
                        host={imapHost}
                        selfHosted={selfHosted}
                        onChange={(v) => {
                            imapSecurityTouched.current = true;
                            setImapSecurity(v);
                        }}
                    />
                </Field>
                <Field label="Username">
                    <TextInput
                        value={imapUser}
                        onChange={(v) => {
                            imapUserTouched.current = true;
                            setImapUser(v);
                        }}
                        placeholder={email || "alex@company.com"}
                    />
                </Field>
                <Field label="Password">
                    <TextInput value={imapPass} onChange={setImapPass} placeholder="App password" type="password" />
                </Field>
            </Section>

            <Section title="SMTP" sub="Outgoing, usually 587 or 465" icon={<SendIcon className="w-3.5 h-3.5" />}>
                <Field label="Server">
                    <HostPortInput
                        host={smtpHost}
                        onHost={setSmtpHost}
                        hostPlaceholder="smtp.example.com"
                        port={smtpPort}
                        onPort={setSmtpPort}
                        portPlaceholder="587"
                    />
                </Field>
                <Field label="Security">
                    <SecuritySelect
                        value={smtpSecurity}
                        host={smtpHost}
                        selfHosted={selfHosted}
                        onChange={(v) => {
                            smtpSecurityTouched.current = true;
                            setSmtpSecurity(v);
                        }}
                    />
                </Field>
                <label className="flex items-center gap-2 pl-[76px] pt-0.5 cursor-pointer">
                    <Checkbox tone="slate"
                        checked={sameCreds}
                        onChange={(e) => setSameCreds(e.target.checked)}
                    />
                    <span className="text-[11.5px] text-slate-600">
                        Use the same login as IMAP
                    </span>
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
                                    <TextInput
                                        value={smtpUser}
                                        onChange={(v) => {
                                            smtpUserTouched.current = true;
                                            setSmtpUser(v);
                                        }}
                                        placeholder={email || "alex@company.com"}
                                    />
                                </Field>
                                <Field label="Password">
                                    <TextInput value={smtpPass} onChange={setSmtpPass} placeholder="App password" type="password" />
                                </Field>
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>
            </Section>

            <CertificateTrustOption show={certFailed || trustCert} checked={trustCert} onChange={setTrustCert} />

            <div className="px-4 py-2.5 border-t border-slate-200 bg-slate-50/60 flex items-center gap-2 min-w-0 sticky bottom-0">
                <div className="flex items-center gap-1.5 text-[11px] text-slate-500 min-w-0 flex-1">
                    <KeyRoundIcon className="w-3 h-3 shrink-0" />
                    <span className="truncate">Verified against your server before saving.</span>
                </div>
                <motion.button
                    type="button"
                    onClick={submit}
                    disabled={!valid() || submitting}
                    whileTap={valid() && !submitting ? { scale: 0.97 } : undefined}
                    className={cn(
                        "shrink-0 h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                        "bg-slate-900 hover:bg-slate-800 text-white disabled:opacity-50 disabled:cursor-not-allowed",
                    )}
                >
                    {submitting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                    Connect
                </motion.button>
            </div>
        </div>
    );
}

function Section({
    title,
    sub,
    icon,
    children,
}: {
    title: string;
    sub: string;
    icon: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <div className="px-4 py-3 border-b border-slate-200/60 last:border-b-0 min-w-0">
            <div className="flex items-center gap-1.5 mb-2 min-w-0">
                <span className="text-slate-500 shrink-0">{icon}</span>
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">
                    {title}
                </span>
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
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-16 shrink-0">
                {label}
            </span>
            <div className="flex-1 min-w-0">{children}</div>
        </div>
    );
}

// HostPortInput — one bordered field that holds host (flex) and port
// (fixed 56px) with a hairline divider between them. Treating it as a
// single visual input avoids the old "Port label + tiny input" pinch
// that was overflowing on narrow modal widths.
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
