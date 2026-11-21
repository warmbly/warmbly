// Connecting (or reconnecting) a LinkedIn account: price, the automation acknowledgement, sign-in, whatever
// LinkedIn asks next, and the connected account. Realtime moves the session on; a waiting checkpoint also re-reads.
import { useEffect, useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import {
    AlertCircleIcon,
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    CookieIcon,
    CreditCardIcon,
    ExternalLinkIcon,
    KeyRoundIcon,
    LayersIcon,
    Loader2Icon,
    LockIcon,
    ShieldAlertIcon,
    SmartphoneIcon,
    WalletIcon,
} from "lucide-react";
import LeadDialog from "@/components/app/leads/LeadDialog";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { Checkbox } from "@/components/ui/checkbox";
import { Label, TextInput } from "@/components/ui/field";
import RollingNumber from "@/components/ui/RollingNumber";
import { useConfirm } from "@/hooks/context/confirm";
import {
    useAnswerConnection,
    useCancelConnection,
    useChooseConnectionMethod,
    useConnection,
    useConnectQuote,
    useCreateConnection,
    useResendConnectionCode,
    useSignInConnection,
} from "@/lib/api/hooks/app/linkedin/useLinkedIn";
import { useAddPaymentMethod } from "@/lib/api/hooks/app/subscription/useBalance";
import type { ConnectCheckpoint, ConnectChoice, ConnectSession, LinkedInAccount, LinkedInStatus } from "@/lib/api/models/app/linkedin/LinkedIn";
import { balanceLine, paymentPlan } from "@/lib/buy/checkout";
import { cardLabel, fmtMicros, fmtMonthly } from "@/lib/buy/money";
import { acceptQuoteMicros, attemptsRemaining, checkoutUrl, linkedInErrorMessage } from "@/lib/linkedin/errors";
import { cn } from "@/lib/utils";
import LinkedInMark from "./LinkedInMark";
import { Avatar, LicenceChips, TIER_LABEL } from "./parts";

type Step = "price" | "ack" | "sign_in" | "verify" | "done" | "failed";
const ORDER: Step[] = ["price", "ack", "sign_in", "verify", "done", "failed"];

const pane = {
    enter: (dir: number) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: number) => ({ x: dir * -28, opacity: 0 }),
};

const TERMINAL = new Set(["succeeded", "failed", "cancelled", "expired"]);

function localTimezone(): string {
    try {
        return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
    } catch {
        return "UTC";
    }
}

function stepOf(s: ConnectSession | undefined, signingIn: boolean): Step | null {
    if (!s) return null;
    if (s.status === "succeeded") return "done";
    if (s.status === "failed" || s.status === "cancelled" || s.status === "expired") return "failed";
    if (s.status === "checkpoint" && !signingIn) return "verify";
    return "sign_in";
}

export default function ConnectDialog({
    open,
    onClose,
    status,
    reconnect,
}: {
    open: boolean;
    onClose: () => void;
    status: LinkedInStatus | undefined;
    /** The account being reconnected; a reconnect is never charged and skips the price. */
    reconnect?: LinkedInAccount | null;
}) {
    const confirm = useConfirm();
    const accountId = reconnect?.id ?? null;
    const first: Step = accountId ? "ack" : "price";

    const [step, setStep] = useState<Step>(first);
    const [dir, setDir] = useState(1);
    const [sessionId, setSessionId] = useState<string | null>(null);
    const [charged, setCharged] = useState(0);
    const [acknowledged, setAcknowledged] = useState(false);
    const [signingIn, setSigningIn] = useState(false);
    const [idemKey, setIdemKey] = useState(() => crypto.randomUUID());
    const [dirty, setDirty] = useState(false);

    // A fresh dialog every time it opens.
    useEffect(() => {
        if (!open) return;
        setStep(first);
        setDir(1);
        setSessionId(null);
        setCharged(0);
        setAcknowledged(false);
        setSigningIn(false);
        setIdemKey(crypto.randomUUID());
        setDirty(false);
    }, [open, first]);

    const quote = useConnectQuote(accountId, open && !accountId);
    const session = useConnection(open ? sessionId : null);
    const create = useCreateConnection();
    const cancel = useCancelConnection();

    // The session decides where the flow is once it exists.
    const sessionStep = stepOf(session.data, signingIn);
    useEffect(() => {
        if (!sessionStep || sessionStep === step) return;
        setDir(ORDER.indexOf(sessionStep) >= ORDER.indexOf(step) ? 1 : -1);
        setStep(sessionStep);
        if (sessionStep !== "sign_in") setSigningIn(false);
    }, [sessionStep, step]);

    const go = (next: Step) => {
        setDir(ORDER.indexOf(next) >= ORDER.indexOf(step) ? 1 : -1);
        setStep(next);
    };

    const live = !!session.data && !TERMINAL.has(session.data.status);

    const requestClose = () => {
        if (live && sessionId) {
            confirm.show(
                "Stop connecting this account? You can start again at any time.",
                async () => {
                    await cancel.mutateAsync(sessionId).catch(() => undefined);
                    onClose();
                },
                undefined,
                { title: "Stop connecting", confirmLabel: "Stop", tone: "danger" },
            );
            return;
        }
        if (dirty && step !== "done") {
            confirm.show("Discard what you entered?", async () => onClose(), undefined, { title: "Discard", confirmLabel: "Discard", tone: "danger" });
            return;
        }
        onClose();
    };

    const free = !!accountId || quote.data?.free === true || status?.mode === "linked";
    const due = quote.data?.due_now_micros ?? null;
    const payment = quote.data?.payment;
    const needsCard = !free && payment?.needs_payment_method === true;
    const planBlocked = !accountId && status?.can_buy === false && status?.buy_blocked_reason === "plan_required";
    const partnerBlocked = !accountId && status?.can_buy === false && status?.buy_blocked_reason === "partner_managed";

    const priceBlocker = planBlocked
        ? "A Warmbly plan is needed first."
        : partnerBlocked
          ? "Your partner adds LinkedIn accounts for this workspace."
          : quote.isPending
            ? null
            : quote.isError
              ? linkedInErrorMessage(quote.error)
              : needsCard
                ? "Add a card to pay for the seat."
                : null;

    // accept is the charge the person saw; a server that prices it differently answers linkedin_accept_required with its quote.
    const startSession = (accept = !free) => {
        const key = accept === !free ? idemKey : crypto.randomUUID();
        if (key !== idemKey) setIdemKey(key);
        create.mutate(
            { body: { account_id: accountId, timezone: localTimezone(), accept, acknowledged: true }, idempotencyKey: key },
            {
                onSuccess: (r) => {
                    setCharged(r.charged_micros);
                    setSessionId(r.session.id);
                    go(stepOf(r.session, false) ?? "sign_in");
                },
            },
        );
    };

    const title = accountId ? "Reconnect LinkedIn" : "Connect LinkedIn";
    const eyebrow = accountId ? "Reconnect" : "LinkedIn";

    let footer: React.ReactNode = null;
    if (step === "price") {
        footer = (
            <>
                {priceBlocker && <span className="mr-auto text-[11.5px] text-slate-500 leading-snug">{priceBlocker}</span>}
                <button type="button" onClick={requestClose} className={secondary}>
                    Cancel
                </button>
                <button type="button" disabled={!!priceBlocker || quote.isPending} onClick={() => go("ack")} className={primary}>
                    Continue <ArrowRightIcon className="w-3.5 h-3.5" />
                </button>
            </>
        );
    } else if (step === "ack") {
        const payLabel = free || !due ? "Continue" : `Pay ${fmtMicros(due)} and continue`;
        footer = (
            <>
                {!acknowledged && <span className="mr-auto text-[11.5px] text-slate-500 leading-snug">Tick the box to go on.</span>}
                {!accountId ? (
                    <button type="button" onClick={() => go("price")} className={secondary}>
                        <ArrowLeftIcon className="w-3.5 h-3.5" /> Back
                    </button>
                ) : (
                    <button type="button" onClick={requestClose} className={secondary}>
                        Cancel
                    </button>
                )}
                <button type="button" disabled={!acknowledged || create.isPending} onClick={() => startSession()} className={primary}>
                    {create.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                    {payLabel}
                </button>
            </>
        );
    } else if (step === "done") {
        footer = (
            <button type="button" onClick={onClose} className={primary}>
                Done
            </button>
        );
    } else if (step === "failed") {
        footer = (
            <>
                <button type="button" onClick={onClose} className={secondary}>
                    Close
                </button>
                <button
                    type="button"
                    onClick={() => {
                        setSessionId(null);
                        setIdemKey(crypto.randomUUID());
                        setAcknowledged(false);
                        go("ack");
                    }}
                    className={primary}
                >
                    Start again
                </button>
            </>
        );
    } else {
        footer = (
            <button type="button" onClick={requestClose} className={secondary}>
                Cancel
            </button>
        );
    }

    return (
        <LeadDialog open={open} onClose={requestClose} icon={<LinkedInMark className="w-3.5 h-3.5" />} eyebrow={eyebrow} title={title} footer={footer} width={500}>
            <Progress step={step} skipPrice={!!accountId} />
            <div className="relative overflow-hidden">
                <AnimatePresence mode="wait" custom={dir} initial={false}>
                    <motion.div
                        key={step}
                        custom={dir}
                        variants={pane}
                        initial="enter"
                        animate="center"
                        exit="exit"
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        className="px-4 py-4"
                    >
                        {step === "price" && (
                            <PriceStep
                                status={status}
                                loading={quote.isPending}
                                error={quote.isError ? quote.error : null}
                                free={free}
                                monthly={quote.data?.monthly_micros ?? status?.price.monthly_micros ?? 0}
                                tier={quote.data?.tier ?? status?.price.tier ?? "standard"}
                                due={due}
                                payment={payment}
                                planBlocked={planBlocked}
                                partnerBlocked={partnerBlocked}
                            />
                        )}
                        {step === "ack" && (
                            <AckStep
                                checked={acknowledged}
                                onChange={setAcknowledged}
                                error={create.isError ? create.error : null}
                                reconnect={reconnect ?? null}
                                pending={create.isPending}
                                onAccept={() => startSession(true)}
                            />
                        )}
                        {step === "sign_in" && sessionId && (
                            <SignInStep sessionId={sessionId} session={session.data} onDirty={() => setDirty(true)} onSubmitted={() => setSigningIn(false)} />
                        )}
                        {step === "verify" && session.data?.checkpoint && sessionId && (
                            <CheckpointStep sessionId={sessionId} session={session.data} checkpoint={session.data.checkpoint} onSignInAgain={() => setSigningIn(true)} />
                        )}
                        {step === "done" && <DoneStep account={session.data?.account ?? null} reconnect={!!accountId} />}
                        {step === "failed" && <FailedStep session={session.data} charged={charged} />}
                    </motion.div>
                </AnimatePresence>
            </div>
        </LeadDialog>
    );
}

const primary =
    "h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 disabled:bg-slate-200 disabled:text-slate-400 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-300";
const secondary =
    "h-7 px-3 rounded-md border border-slate-200 bg-white hover:bg-slate-50 text-slate-700 text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors";

function Progress({ step, skipPrice }: { step: Step; skipPrice: boolean }) {
    const steps = (skipPrice ? ["ack", "sign_in", "verify", "done"] : ["price", "ack", "sign_in", "verify", "done"]) as Step[];
    const labels: Record<string, string> = { price: "Price", ack: "Terms", sign_in: "Sign in", verify: "Verify", done: "Done" };
    const at = step === "failed" ? steps.length - 1 : steps.indexOf(step);
    return (
        <ol className="px-4 pt-3 flex items-center gap-1.5" aria-label="Steps">
            {steps.map((s, i) => (
                <li key={s} className="flex-1 min-w-0">
                    <div className={cn("h-0.5 rounded-full transition-colors", i <= at ? (step === "failed" ? "bg-red-400" : "bg-sky-500") : "bg-slate-200")} />
                    <span className={cn("mt-1 block text-[10px] uppercase tracking-[0.14em] truncate", i === at ? "text-slate-700 font-medium" : "text-slate-400")}>{labels[s]}</span>
                </li>
            ))}
        </ol>
    );
}

function PriceStep({
    status,
    loading,
    error,
    free,
    monthly,
    tier,
    due,
    payment,
    planBlocked,
    partnerBlocked,
}: {
    status: LinkedInStatus | undefined;
    loading: boolean;
    error: unknown;
    free: boolean;
    monthly: number;
    tier: string;
    due: number | null;
    payment: Parameters<typeof paymentPlan>[0];
    planBlocked: boolean;
    partnerBlocked: boolean;
}) {
    const addCard = useAddPaymentMethod();
    if (planBlocked) {
        return (
            <div className="rounded-lg border border-slate-200 p-3.5 flex items-start gap-3">
                <span className="size-9 rounded-full bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                    <LayersIcon className="w-4 h-4" />
                </span>
                <div className="min-w-0">
                    <p className="text-[13px] font-semibold text-slate-900">LinkedIn accounts need a Warmbly plan</p>
                    <p className="mt-1 text-[12px] text-slate-500 leading-relaxed">
                        A seat runs your LinkedIn outreach next to your email campaigns, which a plan covers. You can also use your own GraphKit workspace on any plan.
                    </p>
                    <Link
                        to="/app/settings/billing/{-$tab}"
                        params={{ tab: "plans" }}
                        className="mt-3 h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5"
                    >
                        Choose a plan <ArrowRightIcon className="w-3.5 h-3.5" />
                    </Link>
                </div>
            </div>
        );
    }
    if (partnerBlocked) {
        return <Notice tone="info">Your partner manages this workspace and adds LinkedIn accounts for it. Ask them for a seat.</Notice>;
    }
    if (error) return <Notice tone="error">{linkedInErrorMessage(error)}</Notice>;

    const plan = payment ? paymentPlan(payment, payment.balance_micros, due) : null;
    const paidHow = plan && due ? balanceLine(plan, due) : null;

    return (
        <div className="rounded-lg border border-slate-200 overflow-hidden">
            <div className="px-3.5 py-3 flex items-center gap-3">
                <LinkedInMark className="w-7 h-7" />
                <div className="min-w-0 flex-1">
                    <p className="text-[12.5px] font-medium text-slate-900">One LinkedIn account</p>
                    <p className="text-[11.5px] text-slate-500">
                        {free ? "Billed by your GraphKit workspace" : `${TIER_LABEL[tier] ?? tier} seat · ${fmtMonthly(monthly)}`}
                    </p>
                </div>
                {loading ? <span className="h-3.5 w-14 rounded skeleton-shimmer" /> : <span className="text-[12.5px] font-medium tabular-nums text-slate-900">{free ? "Free" : fmtMicros(monthly)}</span>}
            </div>
            <div className="px-3.5 py-3 border-t border-slate-100 bg-slate-50/60">
                <div className="flex items-end justify-between gap-3">
                    <span className="pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-500 font-medium">Due today</span>
                    <span className="text-[24px] font-semibold tracking-[-0.02em] text-slate-900 tabular-nums leading-none">
                        {free ? fmtMicros(0) : due === null ? <span className="inline-block h-6 w-20 rounded skeleton-shimmer align-bottom" /> : <RollingNumber value={due} format={fmtMicros} />}
                    </span>
                </div>
                {!free && (
                    <div className="mt-2 space-y-1">
                        <p className="text-[12px] text-slate-700 leading-snug">
                            Then {fmtMonthly(monthly)}
                            {payment?.card ? `, charged to ${cardLabel(payment.card)} after your balance` : ", from your balance first and your card for the rest"}
                        </p>
                        {paidHow && <p className="text-[11px] text-slate-500 leading-snug">{paidHow}</p>}
                        {plan && plan.cardCharge > 0 && plan.fromBalance > 0 && !paidHow && (
                            <p className="text-[11px] text-slate-500 leading-snug">
                                {fmtMicros(plan.fromBalance)} from your balance, {fmtMicros(plan.cardCharge)} on the card
                            </p>
                        )}
                    </div>
                )}
                {status?.mode !== "linked" && !free && (
                    <p className="mt-2 text-[11px] text-slate-500 leading-snug">Remove the account at any time; the month you paid for stays yours.</p>
                )}
            </div>
            {needsCardRow(free, payment) && (
                <div className="px-3.5 py-3 border-t border-slate-100 flex items-start gap-2.5">
                    <CreditCardIcon className="w-4 h-4 text-slate-500 mt-0.5 shrink-0" />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12px] text-slate-800 font-medium">Add a card to pay for this seat</p>
                        <p className="text-[11.5px] text-slate-500 leading-snug">Your balance does not cover it. The card pays the rest today and each month after.</p>
                    </div>
                    <button type="button" onClick={() => addCard.mutate()} disabled={addCard.isPending} className={secondary}>
                        {addCard.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <LockIcon className="w-3.5 h-3.5" />}
                        Add card
                    </button>
                </div>
            )}
            {!free && payment && !payment.needs_payment_method && plan?.balanceCovers && (
                <div className="px-3.5 py-2.5 border-t border-slate-100 flex items-center gap-2 text-[11.5px] text-slate-600">
                    <WalletIcon className="w-3.5 h-3.5 text-emerald-600" /> Paid from your balance
                </div>
            )}
        </div>
    );
}

const needsCardRow = (free: boolean, p: Parameters<typeof paymentPlan>[0]) => !free && p?.needs_payment_method === true;

function AckStep({
    checked,
    onChange,
    error,
    reconnect,
    pending,
    onAccept,
}: {
    checked: boolean;
    onChange: (v: boolean) => void;
    error: unknown;
    reconnect: LinkedInAccount | null;
    pending: boolean;
    onAccept: () => void;
}) {
    const url = checkoutUrl(error);
    const quote = acceptQuoteMicros(error);
    return (
        <div className="space-y-3">
            {reconnect && (
                <div className="flex items-center gap-2.5 rounded-lg border border-slate-200 px-3 py-2.5">
                    <Avatar src={reconnect.avatar_url} name={reconnect.display_name} size={28} />
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-slate-900 truncate">{reconnect.display_name}</p>
                        <p className="text-[11px] text-slate-500 truncate">Sign in again to keep this account running. Nothing is charged.</p>
                    </div>
                </div>
            )}
            <div className="rounded-lg border border-amber-200 bg-amber-50/60 px-3.5 py-3">
                <div className="flex items-start gap-2.5">
                    <ShieldAlertIcon className="w-4 h-4 text-amber-600 mt-0.5 shrink-0" />
                    <div className="min-w-0 space-y-1.5 text-[12px] text-amber-950 leading-relaxed">
                        <p className="font-medium">Automating LinkedIn carries a risk to the account</p>
                        <p>
                            LinkedIn&apos;s terms do not allow tools that act on an account for you. Warmbly keeps to modest daily limits, your working hours and a slow
                            start, but LinkedIn can still ask the account to verify itself, limit what it can do, or restrict it.
                        </p>
                        <p>Connect an account you control and are comfortable using this way. You can pause or remove it at any time.</p>
                    </div>
                </div>
            </div>
            <label className="flex items-start gap-2.5 cursor-pointer select-none">
                <Checkbox tone="slate" checked={checked} onChange={(e) => onChange(e.target.checked)} className="mt-0.5" />
                <span className="text-[12px] text-slate-700 leading-relaxed">I understand LinkedIn may restrict an automated account, and I accept that risk for this account.</span>
            </label>
            {quote !== null ? (
                <div role="alert" className="rounded-lg border border-sky-200 bg-sky-50/70 px-3 py-2.5 flex items-start gap-2.5">
                    <CreditCardIcon className="w-4 h-4 text-sky-700 mt-0.5 shrink-0" />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12px] font-medium text-sky-950">This account is charged {fmtMonthly(quote)}</p>
                        <p className="text-[11.5px] text-sky-900/80 leading-snug">
                            The first month is taken now, from your balance first and your card for the rest. It goes back to your balance if the sign-in does not finish.
                        </p>
                    </div>
                    <button type="button" disabled={!checked || pending} onClick={onAccept} className={cn(primary, "shrink-0")}>
                        {pending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        Accept and pay {fmtMicros(quote)}
                    </button>
                </div>
            ) : !!error && (
                <Notice tone="error">
                    {linkedInErrorMessage(error)}
                    {url && (
                        <a href={url} target="_blank" rel="noopener noreferrer" className="ml-1 inline-flex items-center gap-1 font-medium underline underline-offset-2">
                            Open GraphKit <ExternalLinkIcon className="w-3 h-3" />
                        </a>
                    )}
                </Notice>
            )}
        </div>
    );
}

function SignInStep({ sessionId, session, onDirty, onSubmitted }: { sessionId: string; session: ConnectSession | undefined; onDirty: () => void; onSubmitted: () => void }) {
    const signIn = useSignInConnection();
    const [cookie, setCookie] = useState(false);
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [liAt, setLiAt] = useState("");
    const [liA, setLiA] = useState("");
    const [recruiter, setRecruiter] = useState(false);

    const ready = cookie ? liAt.trim().length > 10 : username.trim().length > 2 && password.length > 0;
    const submit = () => {
        if (!ready || signIn.isPending) return;
        const body = cookie
            ? { method: "cookie" as const, li_at: liAt.trim(), ...(liA.trim() ? { li_a: liA.trim() } : {}), recruiter }
            : { method: "password" as const, username: username.trim(), password, recruiter };
        signIn.mutate({ id: sessionId, body }, { onSuccess: () => onSubmitted() });
    };
    const left = attemptsRemaining(signIn.error);
    const edit = (fn: (v: string) => void) => (v: string) => {
        fn(v);
        onDirty();
    };

    return (
        <form
            className="space-y-3"
            onSubmit={(e) => {
                e.preventDefault();
                submit();
            }}
        >
            <div className="inline-flex rounded-md border border-slate-200 p-0.5 bg-slate-50" role="tablist" aria-label="How to sign in">
                {[
                    { id: false, label: "Email and password", icon: KeyRoundIcon },
                    { id: true, label: "Use a session cookie", icon: CookieIcon },
                ].map((o) => (
                    <button
                        key={String(o.id)}
                        type="button"
                        role="tab"
                        aria-selected={cookie === o.id}
                        onClick={() => setCookie(o.id)}
                        className={cn(
                            "h-6 px-2 rounded text-[11.5px] inline-flex items-center gap-1.5 transition-colors",
                            cookie === o.id ? "bg-white text-slate-900 font-medium shadow-sm ring-1 ring-slate-200" : "text-slate-500 hover:text-slate-800",
                        )}
                    >
                        <o.icon className="w-3 h-3" /> {o.label}
                    </button>
                ))}
            </div>

            {cookie ? (
                <div className="space-y-2.5">
                    <div>
                        <Label htmlFor="li-at">li_at cookie</Label>
                        <TextInput id="li-at" value={liAt} onChange={edit(setLiAt)} placeholder="AQEDAR…" autoComplete="off" className="w-full font-mono" autoFocus />
                    </div>
                    <div>
                        <Label htmlFor="li-a">li_a cookie (Sales Navigator or Recruiter, optional)</Label>
                        <TextInput id="li-a" value={liA} onChange={edit(setLiA)} placeholder="AQJ2…" autoComplete="off" className="w-full font-mono" />
                    </div>
                    <p className="text-[11px] text-slate-500 leading-relaxed">
                        In a browser signed in to LinkedIn, open the developer tools, then Application, Cookies, linkedin.com, and copy the value of li_at.
                    </p>
                </div>
            ) : (
                <div className="space-y-2.5">
                    <div>
                        <Label htmlFor="li-user">LinkedIn email</Label>
                        <TextInput id="li-user" value={username} onChange={edit(setUsername)} type="email" autoComplete="username" placeholder="you@company.com" className="w-full" autoFocus />
                    </div>
                    <div>
                        <Label htmlFor="li-pass">Password</Label>
                        <TextInput id="li-pass" value={password} onChange={edit(setPassword)} type="password" autoComplete="current-password" className="w-full" />
                    </div>
                </div>
            )}

            <div className="flex items-start gap-2.5 rounded-lg border border-slate-200 px-3 py-2.5">
                <Toggle value={recruiter} onChange={setRecruiter} ariaLabel="Connect the Recruiter seat instead of Sales Navigator" />
                <div className="min-w-0">
                    <p className="text-[12px] text-slate-800">Connect the Recruiter seat instead of Sales Navigator</p>
                    <p className="text-[11px] text-slate-500 leading-snug">LinkedIn allows one of the two on a connection.</p>
                </div>
            </div>

            <p className="flex items-start gap-1.5 text-[11px] text-slate-500 leading-relaxed">
                <LockIcon className="w-3 h-3 mt-0.5 shrink-0" />
                What you enter goes to LinkedIn once to sign in. Warmbly does not keep your password or cookies.
            </p>

            {(signIn.isError || session?.checkpoint?.last_error) && (
                <Notice tone="error">
                    {signIn.isError ? linkedInErrorMessage(signIn.error) : session?.checkpoint?.last_error}
                    {left !== null && ` ${left === 1 ? "1 attempt" : `${left} attempts`} left.`}
                </Notice>
            )}

            <div className="flex justify-end">
                <button type="submit" disabled={!ready || signIn.isPending} className={primary}>
                    {signIn.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                    Sign in
                </button>
            </div>
        </form>
    );
}

const DELIVERY: Record<string, string> = {
    sms: "by text message",
    email: "by email",
    app: "in the LinkedIn app",
    authenticator: "in your authenticator app",
    totp: "in your authenticator app",
    whatsapp: "on WhatsApp",
    phone: "by phone",
};

const choiceId = (c: ConnectChoice | string) => (typeof c === "string" ? c : c.id);
const choiceLabel = (c: ConnectChoice | string) => (typeof c === "string" ? c.replace(/_/g, " ") : (c.label ?? c.name ?? c.id));

function CheckpointStep({
    sessionId,
    session,
    checkpoint,
    onSignInAgain,
}: {
    sessionId: string;
    session: ConnectSession;
    checkpoint: ConnectCheckpoint;
    onSignInAgain: () => void;
}) {
    const answer = useAnswerConnection();
    const method = useChooseConnectionMethod();
    const resend = useResendConnectionCode();
    const [code, setCode] = useState("");
    const [choice, setChoice] = useState<string | null>(null);
    const type = checkpoint.type;

    // A new checkpoint starts with an empty answer.
    useEffect(() => {
        setCode("");
        setChoice(null);
    }, [type]);

    const err = answer.error ?? method.error;
    const left = checkpoint.attempts_remaining ?? attemptsRemaining(err);
    const problem = (answer.isError || method.isError ? linkedInErrorMessage(err) : null) ?? checkpoint.last_error ?? null;
    const errorLine = problem ? (
        <Notice tone="error">
            {problem}
            {left !== null && left !== undefined && ` ${left === 1 ? "1 attempt" : `${left} attempts`} left.`}
        </Notice>
    ) : null;

    const choices = useMemo(() => checkpoint.choices ?? [], [checkpoint.choices]);

    if (type === "two_factor" || type === "otp" || type === "otp_or_app" || type === "phone_number") {
        const isPhone = type === "phone_number";
        const where = checkpoint.delivery ? (DELIVERY[checkpoint.delivery] ?? `by ${checkpoint.delivery}`) : null;
        const ok = isPhone ? code.replace(/\D/g, "").length >= 6 : code.trim().length >= 4;
        return (
            <form
                className="space-y-3"
                onSubmit={(e) => {
                    e.preventDefault();
                    if (ok) answer.mutate({ id: sessionId, body: { code: code.trim() } });
                }}
            >
                <Heading icon={isPhone ? SmartphoneIcon : KeyRoundIcon} title={isPhone ? "LinkedIn wants a phone number" : "Enter the code LinkedIn sent"}>
                    {isPhone
                        ? "LinkedIn asks for a phone number to send a code to. Use one you can read now, with the country code."
                        : where
                          ? `LinkedIn sent a code ${where}.`
                          : "LinkedIn sent a verification code to the account's owner."}
                </Heading>
                <div>
                    <TextInput
                        value={code}
                        onChange={setCode}
                        placeholder={isPhone ? "+44 7700 900123" : "123456"}
                        autoComplete={isPhone ? "tel" : "one-time-code"}
                        type={isPhone ? "tel" : "text"}
                        className={cn("w-full", !isPhone && "font-mono tracking-[0.3em] text-center")}
                        maxLength={isPhone ? 24 : 12}
                        autoFocus
                    />
                </div>
                {type === "otp_or_app" && <Waiting>Or approve the sign-in in the LinkedIn app on your phone. This moves on by itself.</Waiting>}
                {errorLine}
                <div className="flex items-center justify-between gap-2">
                    {!isPhone ? (
                        <button
                            type="button"
                            disabled={!session.can_resend || resend.isPending}
                            onClick={() => resend.mutate(sessionId)}
                            title={session.can_resend ? undefined : "A new code can be sent 30 seconds after the last one."}
                            className="text-[11.5px] text-sky-700 hover:text-sky-800 disabled:text-slate-400 inline-flex items-center gap-1"
                        >
                            {resend.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            {session.can_resend ? "Send a new code" : "A new code can be sent shortly"}
                        </button>
                    ) : (
                        <span />
                    )}
                    <button type="submit" disabled={!ok || answer.isPending} className={primary}>
                        {answer.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        {isPhone ? "Send code" : "Verify"}
                    </button>
                </div>
            </form>
        );
    }

    if (type === "app_approval") {
        return (
            <div className="space-y-3">
                <Heading icon={SmartphoneIcon} title="Approve the sign-in in the LinkedIn app">
                    LinkedIn sent a prompt to the LinkedIn app on the account owner&apos;s phone. Tap Yes there; this window moves on by itself.
                </Heading>
                <Waiting>Waiting for approval</Waiting>
                {errorLine}
            </div>
        );
    }

    if (type === "choose_contract" || type === "choose_method") {
        const isMethod = type === "choose_method";
        const pending = answer.isPending || method.isPending;
        return (
            <div className="space-y-3">
                <Heading icon={isMethod ? KeyRoundIcon : LayersIcon} title={isMethod ? "How should LinkedIn verify it is you?" : "Which contract should this account use?"}>
                    {isMethod ? "Pick where LinkedIn sends the verification code." : "This account belongs to more than one Sales Navigator or Recruiter contract."}
                </Heading>
                <div role="radiogroup" className="space-y-1.5">
                    {choices.map((c) => {
                        const id = choiceId(c);
                        return (
                            <button
                                key={id}
                                type="button"
                                role="radio"
                                aria-checked={choice === id}
                                onClick={() => setChoice(id)}
                                className={cn(
                                    "w-full h-9 px-3 rounded-md border text-left text-[12.5px] flex items-center gap-2 transition-colors",
                                    choice === id ? "border-sky-400 bg-sky-50 text-sky-900" : "border-slate-200 hover:border-slate-300 text-slate-800",
                                )}
                            >
                                <span className={cn("size-3.5 rounded-full border flex items-center justify-center", choice === id ? "border-sky-600" : "border-slate-300")}>
                                    {choice === id && <span className="size-1.5 rounded-full bg-sky-600" />}
                                </span>
                                <span className="truncate">{choiceLabel(c)}</span>
                            </button>
                        );
                    })}
                    {choices.length === 0 && <p className="text-[12px] text-slate-500">LinkedIn offered no choices. Sign in again.</p>}
                </div>
                {errorLine}
                <div className="flex items-center justify-end gap-2">
                    {choices.length === 0 && (
                        <button type="button" onClick={onSignInAgain} className={secondary}>
                            Sign in again
                        </button>
                    )}
                    <button
                        type="button"
                        disabled={!choice || pending}
                        onClick={() => {
                            if (!choice) return;
                            if (isMethod) method.mutate({ id: sessionId, challenge: choice });
                            else answer.mutate({ id: sessionId, body: { choice } });
                        }}
                        className={primary}
                    >
                        {pending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        Continue
                    </button>
                </div>
            </div>
        );
    }

    if (type === "captcha") {
        const image = checkpoint.captcha_data && checkpoint.captcha_data.startsWith("data:image/") ? checkpoint.captcha_data : null;
        return (
            <div className="space-y-3">
                <Heading icon={ShieldAlertIcon} title="LinkedIn wants a captcha">
                    {image ? "Type the characters in the picture." : "LinkedIn asked for a puzzle that cannot be shown here. Sign in with a session cookie instead, which skips it."}
                </Heading>
                {image && (
                    <>
                        <img src={image} alt="Captcha" className="rounded-md border border-slate-200 max-h-24" />
                        <TextInput value={code} onChange={setCode} placeholder="Characters" className="w-full" autoFocus />
                    </>
                )}
                {errorLine}
                <div className="flex items-center justify-end gap-2">
                    <button type="button" onClick={onSignInAgain} className={secondary}>
                        Sign in again
                    </button>
                    {image && (
                        <button type="button" disabled={!code.trim() || answer.isPending} onClick={() => answer.mutate({ id: sessionId, body: { code: code.trim() } })} className={primary}>
                            {answer.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                            Continue
                        </button>
                    )}
                </div>
            </div>
        );
    }

    return (
        <div className="space-y-3">
            <Heading icon={AlertCircleIcon} title="LinkedIn asked for a check Warmbly cannot complete">
                Sign in once in a browser to clear it, or connect with a session cookie instead.
            </Heading>
            <div className="flex justify-end">
                <button type="button" onClick={onSignInAgain} className={secondary}>
                    Sign in again
                </button>
            </div>
        </div>
    );
}

function DoneStep({ account, reconnect }: { account: LinkedInAccount | null; reconnect: boolean }) {
    return (
        <div className="space-y-3">
            <div className="flex flex-col items-center text-center py-2">
                <motion.span
                    initial={{ scale: 0.6, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ type: "spring", stiffness: 420, damping: 22 }}
                    className="size-10 rounded-full bg-emerald-50 text-emerald-600 inline-flex items-center justify-center"
                >
                    <CheckCircle2Icon className="w-5 h-5" />
                </motion.span>
                <p className="mt-2 text-[13.5px] font-semibold text-slate-900">{reconnect ? "Reconnected" : "LinkedIn connected"}</p>
                <p className="mt-0.5 text-[12px] text-slate-500 max-w-[40ch]">It starts gently and works within its daily limits and working hours. Change them in the account&apos;s settings.</p>
            </div>
            {account && (
                <div className="rounded-lg border border-slate-200 px-3 py-2.5 flex items-center gap-3">
                    <Avatar src={account.avatar_url} name={account.display_name} size={36} />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12.5px] font-medium text-slate-900 truncate">{account.display_name}</p>
                        {account.headline && <p className="text-[11.5px] text-slate-500 truncate">{account.headline}</p>}
                        <LicenceChips licences={account.licences} className="mt-1" />
                    </div>
                </div>
            )}
        </div>
    );
}

function FailedStep({ session, charged }: { session: ConnectSession | undefined; charged: number }) {
    const code = session?.error?.code ?? session?.status;
    const message =
        session?.status === "expired"
            ? "This sign-in timed out. Start again when you are ready."
            : session?.status === "cancelled"
              ? "This sign-in was stopped."
              : code === "sign_in_failed"
                ? "LinkedIn did not accept the sign-in."
                : code === "too_many_failed_sign_ins"
                  ? "Too many refused sign-ins today for this account. Try again tomorrow."
                  : (session?.error?.message ?? "LinkedIn did not finish the sign-in.");
    return (
        <div className="space-y-3">
            <Heading icon={AlertCircleIcon} title="The account did not connect">
                {message}
            </Heading>
            {charged > 0 && <Notice tone="info">The {fmtMicros(charged)} charged for this seat goes back to your balance when a sign-in does not finish.</Notice>}
        </div>
    );
}

function Heading({ icon: Icon, title, children }: { icon: typeof KeyRoundIcon; title: string; children: React.ReactNode }) {
    return (
        <div className="flex items-start gap-2.5">
            <span className="size-8 rounded-full bg-slate-100 text-slate-600 inline-flex items-center justify-center shrink-0">
                <Icon className="w-4 h-4" />
            </span>
            <div className="min-w-0">
                <p className="text-[13px] font-semibold text-slate-900">{title}</p>
                <p className="mt-0.5 text-[12px] text-slate-500 leading-relaxed">{children}</p>
            </div>
        </div>
    );
}

function Waiting({ children }: { children: React.ReactNode }) {
    return (
        <div className="flex items-center gap-2 rounded-md bg-sky-50/70 border border-sky-100 px-3 py-2 text-[11.5px] text-sky-900" role="status" aria-live="polite">
            <span className="relative flex size-2">
                <span className="absolute inline-flex size-full rounded-full bg-sky-400 opacity-75 animate-ping" />
                <span className="relative inline-flex size-2 rounded-full bg-sky-500" />
            </span>
            {children}
        </div>
    );
}

export function Notice({ tone, children }: { tone: "error" | "info"; children: React.ReactNode }) {
    return (
        <div
            role={tone === "error" ? "alert" : "status"}
            className={cn(
                "rounded-lg border px-3 py-2 flex items-start gap-2 text-[11.5px] leading-relaxed",
                tone === "error" ? "border-red-200 bg-red-50/70 text-red-800" : "border-sky-200 bg-sky-50/70 text-sky-900",
            )}
        >
            <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
            <span className="min-w-0">{children}</span>
        </div>
    );
}

