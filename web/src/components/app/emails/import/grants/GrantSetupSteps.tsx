// The guided setup of a new admin grant, one wizard step per task. Google:
// authorize Warmbly in the Admin console, then prove the domain is yours.
// Microsoft: a Global Administrator approves Warmbly once. The draft and the
// popups live in GrantImportWizard, so moving between steps loses nothing.
import React from "react";
import toast from "react-hot-toast/headless";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowLeftIcon, ChevronRightIcon, ClockIcon, ExternalLinkIcon, GlobeIcon, Loader2Icon, ShieldCheckIcon } from "lucide-react";
import { Label, TextInput } from "@/components/ui/field";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { useFinishGoogleGrant, useStartGoogleGrant } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { grantErrorText } from "@/hooks/useAdminGrantPopup";
import type { DomainGrant, GrantConfig } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import { cn } from "@/lib/utils";
import { closePopup } from "@/lib/popup";
import { Banner, CopyValue, SectionLabel } from "../parts";
import type { GoogleDraft } from "./googleDraft";

export const GOOGLE_DELEGATION_URL = "https://admin.google.com/ac/owl/domainwidedelegation";
const MS_ACCESS_POLICY_DOCS = "https://learn.microsoft.com/en-us/graph/auth-limit-mailbox-access";
const DELEGATION_UNAUTHORIZED = "google_delegation_unauthorized";

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

/** Step 1 of a Google domain: the Admin console authorization the grant is checked against. */
export function GoogleAuthorizeStep({ config, onCancel }: { config: GrantConfig | undefined; onCancel?: () => void }) {
    const scopes = config?.google_scopes ?? [];
    return (
        <div className="p-4 space-y-4">
            {onCancel && <CancelSetup label="Back to connected domains" onClick={onCancel} />}
            <StepHeader
                provider="google"
                title="Authorize Warmbly in Google Admin"
                sub="A super admin of the domain does this once. It lets Warmbly act for the mailboxes you pick, with no app passwords."
            />
            <ol className="space-y-3">
                <Instruction n={1}>
                    <p>
                        Sign in to <span className="font-medium text-slate-900">admin.google.com</span> as a super admin.
                    </p>
                    <a
                        href={GOOGLE_DELEGATION_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <ExternalLinkIcon className="w-3 h-3" />
                        Open Google Admin
                    </a>
                </Instruction>
                <Instruction n={2}>
                    <p>Go to</p>
                    <Crumbs items={["Security", "Access and data control", "API controls", "Manage domain-wide delegation"]} />
                    <p>
                        and click <span className="font-medium text-slate-900">Add new</span>.
                    </p>
                </Instruction>
                <Instruction n={3}>
                    <p>Paste the Client ID.</p>
                    {config?.google_client_id ? (
                        <CopyValue label="Client ID" value={config.google_client_id} />
                    ) : (
                        <p className="text-amber-700">This instance did not report its client ID. Ask the operator for it.</p>
                    )}
                </Instruction>
                <Instruction n={4}>
                    <p>Paste the OAuth scopes, exactly as they are.</p>
                    {scopes.length > 0 && (
                        <CopyValue
                            label="OAuth scopes (comma separated)"
                            value={scopes.join(",")}
                            display={
                                <span className="block space-y-0.5">
                                    {scopes.map((sc) => (
                                        <span key={sc} className="block">
                                            {sc}
                                        </span>
                                    ))}
                                </span>
                            }
                        />
                    )}
                </Instruction>
                <Instruction n={5}>
                    <p>
                        Click <span className="font-medium text-slate-900">Authorize</span>.
                    </p>
                </Instruction>
            </ol>
            <p className="flex items-start gap-1.5 text-[11.5px] text-slate-500 leading-relaxed">
                <ClockIcon className="w-3 h-3 text-slate-400 mt-0.5 shrink-0" />
                Google usually applies it within a few minutes. Continue once it is saved; the next step checks it.
            </p>
        </div>
    );
}

const DOMAIN_RE = /^[a-z0-9-]+(\.[a-z0-9-]+)+$/;

function draftProblem(d: string, a: string): string | null {
    if (!DOMAIN_RE.test(d)) return "Enter the domain, like example.com.";
    if (!/^[^\s@]+@[^\s@]+$/.test(a)) return "Enter the super admin's address.";
    if (!a.endsWith(`@${d}`)) return `The super admin's address has to be on ${d}.`;
    return null;
}

/** Step 2 of a Google domain: the super admin signs in, or a DNS record proves it. */
export function GoogleVerifyStep({
    draft,
    signin,
    onGranted,
    onBackToAuthorize,
}: {
    draft: GoogleDraft;
    signin: {
        busy: boolean;
        open: (begin: () => Promise<{ url?: string; state?: string }>, reserved?: Window | null) => Promise<void>;
        reserve: () => Window | null;
    };
    onGranted: (g: DomainGrant) => void;
    onBackToAuthorize: () => void;
}) {
    const startProof = useStartGoogleGrant();
    const checkDns = useFinishGoogleGrant();
    const { domain, setDomain, admin, setAdmin, tried, setTried, proof, setProof, dnsOpen, setDnsOpen, error, setError } = draft;
    const [asking, setAsking] = React.useState<"signin" | "dns" | null>(null);

    const d = domain.trim().toLowerCase().replace(/^@/, "");
    const a = admin.trim().toLowerCase();
    const problem = draftProblem(d, a);
    const live = proof && proof.domain === d && proof.admin === a ? proof : null;
    // Sign-in needs this instance's Google sign-in app; without one, DNS is the only proof.
    const dnsOnly = live?.method === "dns";

    async function ensureProof(): Promise<NonNullable<GoogleDraft["proof"]> | null> {
        setTried(true);
        if (problem) return null;
        if (live) return live;
        setError(null);
        try {
            const out = await startProof.mutateAsync({ domain: d, admin_email: a });
            const p = { ...out, domain: d, admin: a };
            setProof(p);
            return p;
        } catch (err) {
            const e = err as AppError;
            setError({ text: grantErrorText(e, "Google"), code: e?.code });
            return null;
        }
    }

    async function signIn() {
        if (signin.busy || startProof.isPending) return;
        if (problem) {
            setTried(true);
            return;
        }
        // Opened inside the click, before the proof request: Safari blocks a window opened after it.
        const win = dnsOnly ? null : signin.reserve();
        setAsking("signin");
        const p = await ensureProof();
        setAsking(null);
        if (!p) {
            closePopup(win);
            return;
        }
        if (p.method !== "signin" || !p.url) {
            closePopup(win);
            setDnsOpen(true);
            return;
        }
        setError(null);
        // A used state is refused, so a proof from an earlier attempt is replaced.
        const reused = p === live;
        await signin.open(async () => {
            if (!reused) return p;
            const out = await startProof.mutateAsync({ domain: d, admin_email: a });
            setProof({ ...out, domain: d, admin: a });
            return out;
        }, win);
    }

    async function showDns() {
        if (dnsOpen) {
            setDnsOpen(false);
            return;
        }
        if (startProof.isPending) return;
        setAsking("dns");
        const p = await ensureProof();
        setAsking(null);
        if (p) setDnsOpen(true);
    }

    async function checkRecord() {
        if (!live || checkDns.isPending) return;
        setError(null);
        try {
            const g = await checkDns.mutateAsync({ domain: live.domain, admin_email: live.admin });
            toast.success(`${g.tenant} connected`);
            onGranted(g);
        } catch (err) {
            const e = err as AppError;
            setError({ text: grantErrorText(e, "Google"), code: e?.code });
        }
    }

    const signinBusy = signin.busy || asking === "signin";
    return (
        <div className="p-4 space-y-4">
            <StepHeader
                provider="google"
                title="Prove you own the domain"
                sub="Google only confirms who you are; nothing is posted or changed. The domain's user list is then read so you can pick mailboxes."
            />
            <form
                onSubmit={(e) => {
                    e.preventDefault();
                    void signIn();
                }}
                className="space-y-3"
            >
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                    <div>
                        <Label>Domain</Label>
                        <TextInput
                            value={domain}
                            onChange={setDomain}
                            placeholder="example.com"
                            invalid={tried && !!problem && problem.includes("domain")}
                            className="w-full"
                        />
                    </div>
                    <div>
                        <Label>Super admin address</Label>
                        <TextInput
                            value={admin}
                            onChange={setAdmin}
                            placeholder="admin@example.com"
                            autoComplete="email"
                            invalid={tried && !!problem && problem.includes("admin")}
                            className="w-full"
                        />
                    </div>
                </div>
                {tried && problem && <p className="text-[11.5px] text-amber-700">{problem}</p>}
                {!dnsOnly && (
                    <button
                        type="submit"
                        aria-disabled={signinBusy}
                        className={cn(
                            "w-full h-9 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12.5px] font-medium inline-flex items-center justify-center gap-2 transition-colors",
                            signinBusy && "opacity-60",
                        )}
                    >
                        {signinBusy ? (
                            <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                        ) : (
                            <span className="size-5 rounded bg-white inline-flex items-center justify-center">
                                <ProviderLogo id="google" size="xs" framed={false} />
                            </span>
                        )}
                        {signin.busy ? "Waiting for Google…" : "Sign in with Google"}
                    </button>
                )}
            </form>

            {dnsOnly ? (
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    This instance proves domains with a DNS record. Add the record below, then check it.
                </p>
            ) : (
                <button
                    type="button"
                    onClick={() => void showDns()}
                    aria-expanded={dnsOpen}
                    className="h-6 -ml-1 px-1 rounded text-[11.5px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                >
                    {asking === "dns" ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <GlobeIcon className="w-3 h-3" />
                    )}
                    {dnsOpen ? "Hide the DNS record" : "Use a DNS record instead"}
                </button>
            )}

            <AnimatePresence initial={false}>
                {(dnsOpen || dnsOnly) && live && (
                    <motion.div
                        key="dns"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.18 }}
                        className="overflow-hidden"
                    >
                        <div className="rounded-md border border-slate-200 p-3 space-y-2 text-[11.5px] text-slate-600 leading-relaxed">
                            <SectionLabel>DNS record</SectionLabel>
                            <p>
                                Add this TXT record at the DNS provider for {live.domain}. It is unique to this workspace, and you can remove it
                                once the domain is connected.
                            </p>
                            <CopyValue label="Name (TXT)" value={live.txt_name} />
                            <CopyValue label="Value" value={live.txt_value} />
                            <button
                                type="button"
                                onClick={() => void checkRecord()}
                                aria-disabled={checkDns.isPending}
                                className={cn(
                                    "h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                                    checkDns.isPending && "opacity-60",
                                )}
                            >
                                {checkDns.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ShieldCheckIcon className="w-3 h-3" />}
                                {checkDns.isPending ? "Checking…" : "Check the record"}
                            </button>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence initial={false}>
                {error && (
                    <motion.div
                        key={error.code ?? error.text}
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.16 }}
                    >
                        {error.code === DELEGATION_UNAUTHORIZED ? (
                            <Banner tone="amber" title="Google has not accepted the authorization yet">
                                Google has not applied the authorization yet, or the scopes do not match. Check step 1, wait a few minutes,
                                then try again.{" "}
                                <button type="button" onClick={onBackToAuthorize} className="underline font-medium">
                                    Back to step 1
                                </button>
                            </Banner>
                        ) : (
                            <Banner tone="red" title="The domain is not connected yet">
                                {error.text}
                            </Banner>
                        )}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
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
