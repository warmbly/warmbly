// Connecting (or reconnecting) a social account: channel, price, the automation acknowledgement, the channel's sign-in,
// whatever it asks next, and the connected account. Realtime moves the session on; a waiting checkpoint also re-reads.
import { useEffect, useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import { QRCodeSVG } from "qrcode.react";
import {
    AlertCircleIcon,
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    CookieIcon,
    CreditCardIcon,
    ExternalLinkIcon,
    HistoryIcon,
    KeyRoundIcon,
    LayersIcon,
    Loader2Icon,
    LockIcon,
    QrCodeIcon,
    ShieldAlertIcon,
    ShieldCheckIcon,
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
    useSocialChannels,
} from "@/lib/api/hooks/app/social/useSocial";
import { useAddPaymentMethod } from "@/lib/api/hooks/app/subscription/useBalance";
import type { ChannelSpec, ConnectCheckpoint, ConnectChoice, ConnectSession, SignIn, SocialAccount, SocialChannel, SocialStatus } from "@/lib/api/models/app/social/Social";
import { balanceLine, paymentPlan } from "@/lib/buy/checkout";
import { cardLabel, fmtMicros, fmtMonthly } from "@/lib/buy/money";
import { AUTH_METHOD_LABEL, CHANNELS, channelLabel, orderedCatalog } from "@/lib/social/channels";
import { acceptQuoteMicros, attemptsRemaining, checkoutUrl, socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import ChannelMark from "./ChannelMark";
import { Avatar, LicenceChips, TIER_LABEL } from "./parts";

type Step = "channel" | "price" | "ack" | "sign_in" | "verify" | "done" | "failed";
const ORDER: Step[] = ["channel", "price", "ack", "sign_in", "verify", "done", "failed"];

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
    initialChannel,
}: {
    open: boolean;
    onClose: () => void;
    status: SocialStatus | undefined;
    /** The account being reconnected; a reconnect is never charged and skips the channel and the price. */
    reconnect?: SocialAccount | null;
    /** Opens on this channel and skips the channel step (a setup link's ?connect=<channel>). */
    initialChannel?: SocialChannel | null;
}) {
    const confirm = useConfirm();
    const catalog = useSocialChannels(open);
    const accountId = reconnect?.id ?? null;
    const presetChannel = reconnect?.channel ?? initialChannel ?? null;
    const first: Step = accountId ? "ack" : presetChannel ? "price" : "channel";

    const [channel, setChannel] = useState<SocialChannel | null>(presetChannel);
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
        setChannel(presetChannel);
        setStep(first);
        setDir(1);
        setSessionId(null);
        setCharged(0);
        setAcknowledged(false);
        setSigningIn(false);
        setIdemKey(crypto.randomUUID());
        setDirty(false);
    }, [open, first, presetChannel]);

    const spec = catalog.data?.find((c) => c.channel === channel);
    const name = channel ? channelLabel(channel, catalog.data) : "social";

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
          ? "Your partner adds social accounts for this workspace."
          : quote.isPending
            ? null
            : quote.isError
              ? socialErrorMessage(quote.error)
              : needsCard
                ? "Add a card to pay for the account."
                : null;

    // accept is the charge the person saw; a server that prices it differently answers social_accept_required with its quote.
    const startSession = (accept = !free) => {
        if (!channel) return;
        const key = accept === !free ? idemKey : crypto.randomUUID();
        if (key !== idemKey) setIdemKey(key);
        create.mutate(
            { body: { channel, account_id: accountId, timezone: localTimezone(), accept, acknowledged: true }, idempotencyKey: key },
            {
                onSuccess: (r) => {
                    setCharged(r.charged_micros);
                    setSessionId(r.session.id);
                    go(stepOf(r.session, false) ?? "sign_in");
                },
            },
        );
    };

    const title = channel ? `${accountId ? "Reconnect" : "Connect"} ${name}` : "Connect a social account";
    const eyebrow = accountId ? "Reconnect" : "Social accounts";

    let footer: React.ReactNode = null;
    if (step === "channel") {
        footer = (
            <>
                {!channel && <span className="mr-auto text-[11.5px] text-slate-500 leading-snug">Pick a channel to go on.</span>}
                <button type="button" onClick={requestClose} className={secondary}>
                    Cancel
                </button>
                <button type="button" disabled={!channel || !spec} onClick={() => go("price")} className={primary}>
                    Continue <ArrowRightIcon className="w-3.5 h-3.5" />
                </button>
            </>
        );
    } else if (step === "price") {
        footer = (
            <>
                {priceBlocker && <span className="mr-auto text-[11.5px] text-slate-500 leading-snug">{priceBlocker}</span>}
                {presetChannel ? (
                    <button type="button" onClick={requestClose} className={secondary}>
                        Cancel
                    </button>
                ) : (
                    <button type="button" onClick={() => go("channel")} className={secondary}>
                        <ArrowLeftIcon className="w-3.5 h-3.5" /> Back
                    </button>
                )}
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
        <LeadDialog
            open={open}
            onClose={requestClose}
            icon={channel ? <ChannelMark channel={channel} className="w-3.5 h-3.5" /> : <QrCodeIcon className="w-3.5 h-3.5 text-slate-500" />}
            eyebrow={eyebrow}
            title={title}
            footer={footer}
            width={500}
        >
            <Progress step={step} skipChannel={!!presetChannel} skipPrice={!!accountId} />
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
                        {step === "channel" && (
                            <ChannelStep
                                catalog={catalog.data}
                                loading={catalog.isPending}
                                error={catalog.isError ? catalog.error : null}
                                value={channel}
                                onChange={(c) => {
                                    setChannel(c);
                                    setDirty(true);
                                }}
                                onPick={(c) => {
                                    setChannel(c);
                                    go("price");
                                }}
                            />
                        )}
                        {step === "price" && (
                            <PriceStep
                                name={name}
                                channel={channel}
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
                                name={name}
                                spec={spec}
                                checked={acknowledged}
                                onChange={setAcknowledged}
                                error={create.isError ? create.error : null}
                                reconnect={reconnect ?? null}
                                pending={create.isPending}
                                onAccept={() => startSession(true)}
                            />
                        )}
                        {step === "sign_in" && sessionId && channel && (
                            <SignInStep
                                sessionId={sessionId}
                                session={session.data}
                                channel={channel}
                                name={name}
                                spec={spec}
                                onDirty={() => setDirty(true)}
                                onSubmitted={() => setSigningIn(false)}
                            />
                        )}
                        {step === "verify" && session.data?.checkpoint && sessionId && (
                            <CheckpointStep sessionId={sessionId} session={session.data} checkpoint={session.data.checkpoint} name={name} onSignInAgain={() => setSigningIn(true)} />
                        )}
                        {step === "done" && <DoneStep account={session.data?.account ?? null} reconnect={!!accountId} name={name} />}
                        {step === "failed" && <FailedStep session={session.data} charged={charged} name={name} />}
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

function Progress({ step, skipChannel, skipPrice }: { step: Step; skipChannel: boolean; skipPrice: boolean }) {
    const all: Step[] = ["channel", "price", "ack", "sign_in", "verify", "done"];
    const steps = all.filter((s) => !(s === "channel" && skipChannel) && !(s === "price" && skipPrice));
    const labels: Record<string, string> = { channel: "Channel", price: "Price", ack: "Terms", sign_in: "Sign in", verify: "Verify", done: "Done" };
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

function ChannelStep({
    catalog,
    loading,
    error,
    value,
    onChange,
    onPick,
}: {
    catalog: ChannelSpec[] | undefined;
    loading: boolean;
    error: unknown;
    value: SocialChannel | null;
    onChange: (c: SocialChannel) => void;
    onPick: (c: SocialChannel) => void;
}) {
    if (error) return <Notice tone="error">{socialErrorMessage(error)}</Notice>;
    if (loading) {
        return (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                {CHANNELS.map((c) => (
                    <div key={c} className="h-[76px] rounded-lg skeleton-shimmer" />
                ))}
            </div>
        );
    }
    const list = orderedCatalog(catalog);
    return (
        <div className="space-y-3">
            <p className="text-[12px] text-slate-500 leading-relaxed">Which channel is the account on? Every channel costs the same per account.</p>
            <div role="radiogroup" aria-label="Channel" className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                {list.map((c) => {
                    const active = value === c.channel;
                    return (
                        <button
                            key={c.channel}
                            type="button"
                            role="radio"
                            aria-checked={active}
                            onClick={() => onChange(c.channel)}
                            onDoubleClick={() => onPick(c.channel)}
                            className={cn(
                                "flex flex-col items-start gap-1.5 rounded-lg border px-3 py-2.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200",
                                active ? "border-sky-400 bg-sky-50/60" : "border-slate-200 hover:border-slate-300",
                            )}
                        >
                            <ChannelMark channel={c.channel} className="size-5" />
                            <span className="text-[12.5px] font-medium text-slate-900">{c.label}</span>
                            <span className="text-[10.5px] leading-snug text-slate-500">{c.consent_required ? "Contacts who agreed" : "Cold outreach"}</span>
                        </button>
                    );
                })}
            </div>
            {list.length === 0 && <p className="text-[12px] text-slate-500">No channel is available on this instance right now.</p>}
        </div>
    );
}

function PriceStep({
    name,
    channel,
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
    name: string;
    channel: SocialChannel | null;
    status: SocialStatus | undefined;
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
                    <p className="text-[13px] font-semibold text-slate-900">Social accounts need a Warmbly plan</p>
                    <p className="mt-1 text-[12px] text-slate-500 leading-relaxed">
                        An account runs your {name} outreach next to your email campaigns, which a plan covers. You can also use your own GraphKit workspace on any plan.
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
        return <Notice tone="info">Your partner manages this workspace and adds social accounts for it. Ask them for one.</Notice>;
    }
    if (error) return <Notice tone="error">{socialErrorMessage(error)}</Notice>;

    const plan = payment ? paymentPlan(payment, payment.balance_micros, due) : null;
    const paidHow = plan && due ? balanceLine(plan, due) : null;

    return (
        <div className="rounded-lg border border-slate-200 overflow-hidden">
            <div className="px-3.5 py-3 flex items-center gap-3">
                {channel && <ChannelMark channel={channel} className="w-7 h-7" />}
                <div className="min-w-0 flex-1">
                    <p className="text-[12.5px] font-medium text-slate-900">One {name} account</p>
                    <p className="text-[11.5px] text-slate-500">
                        {free
                            ? status?.mode === "linked"
                                ? "Billed by your GraphKit workspace"
                                : "Seats are not charged on this instance"
                            : `${TIER_LABEL[tier] ?? tier} pricing · ${fmtMonthly(monthly)}`}
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
                        <p className="text-[12px] text-slate-800 font-medium">Add a card to pay for this account</p>
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
    name,
    spec,
    checked,
    onChange,
    error,
    reconnect,
    pending,
    onAccept,
}: {
    name: string;
    spec: ChannelSpec | undefined;
    checked: boolean;
    onChange: (v: boolean) => void;
    error: unknown;
    reconnect: SocialAccount | null;
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
                        <p className="font-medium">Automating {name} carries a risk to the account</p>
                        <p>
                            {name}&apos;s terms do not allow tools that act on an account for you. Warmbly keeps to modest daily limits, your working hours and a slow
                            start, but {name} can still ask the account to verify itself, limit what it can do, or restrict it.
                        </p>
                        <p>Connect an account you control and are comfortable using this way. You can pause or remove it at any time.</p>
                    </div>
                </div>
            </div>
            {spec?.consent_required && (
                <div className="rounded-lg border border-sky-200 bg-sky-50/60 px-3.5 py-2.5 flex items-start gap-2.5">
                    <ShieldCheckIcon className="w-4 h-4 text-sky-700 mt-0.5 shrink-0" />
                    <p className="text-[12px] text-sky-950 leading-relaxed">
                        {name} is not for cold outreach. Campaigns only reach contacts with a consent basis recorded for {name}, and a contact who replies STOP is opted out.
                    </p>
                </div>
            )}
            <label className="flex items-start gap-2.5 cursor-pointer select-none">
                <Checkbox tone="slate" checked={checked} onChange={(e) => onChange(e.target.checked)} className="mt-0.5" />
                <span className="text-[12px] text-slate-700 leading-relaxed">I understand {name} may restrict an automated account, and I accept that risk for this account.</span>
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
                    {socialErrorMessage(error)}
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

const USERNAME_LABEL: Record<string, { label: string; placeholder: string; type: "email" | "text" }> = {
    linkedin: { label: "LinkedIn email", placeholder: "you@company.com", type: "email" },
    instagram: { label: "Username, email or phone", placeholder: "janedoe", type: "text" },
    facebook: { label: "Email or phone", placeholder: "you@company.com", type: "text" },
    telegram: { label: "Phone number", placeholder: "+44 7700 900123", type: "text" },
    whatsapp: { label: "Phone number", placeholder: "+44 7700 900123", type: "text" },
};

const METHOD_ICON: Record<string, typeof KeyRoundIcon> = { password: KeyRoundIcon, cookie: CookieIcon, qr: QrCodeIcon, pairing_code: SmartphoneIcon };

function SignInStep({
    sessionId,
    session,
    channel,
    name,
    spec,
    onDirty,
    onSubmitted,
}: {
    sessionId: string;
    session: ConnectSession | undefined;
    channel: SocialChannel;
    name: string;
    spec: ChannelSpec | undefined;
    onDirty: () => void;
    onSubmitted: () => void;
}) {
    const signIn = useSignInConnection();
    const methods = useMemo(() => (spec?.auth_methods ?? []).filter((m) => m in AUTH_METHOD_LABEL), [spec?.auth_methods]);
    const [method, setMethod] = useState<string>(() => methods[0] ?? "password");
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [cookies, setCookies] = useState<Record<string, string>>({});
    const [phone, setPhone] = useState("");
    const [recruiter, setRecruiter] = useState(false);

    // The catalog may arrive after the step opens.
    useEffect(() => {
        if (methods.length && !methods.includes(method)) setMethod(methods[0]);
    }, [methods, method]);

    const required = spec?.cookie_names?.required ?? [];
    const optional = spec?.cookie_names?.optional ?? [];
    const isLinkedIn = channel === "linkedin";
    const user = USERNAME_LABEL[channel] ?? { label: "Username", placeholder: "", type: "text" as const };

    const ready =
        method === "cookie"
            ? required.length > 0 && required.every((n) => (cookies[n] ?? "").trim().length > 10)
            : method === "pairing_code"
              ? phone.replace(/\D/g, "").length >= 7
              : method === "qr"
                ? true
                : username.trim().length > 2 && password.length > 0;

    const submit = () => {
        if (!ready || signIn.isPending) return;
        let body: SignIn;
        if (method === "cookie") {
            const sent: Record<string, string> = {};
            for (const n of [...required, ...optional]) {
                const v = (cookies[n] ?? "").trim();
                if (v) sent[n] = v;
            }
            body = { method: "cookie", cookies: sent, ...(isLinkedIn ? { recruiter } : {}) };
        } else if (method === "pairing_code") {
            body = { method: "pairing_code", phone_number: phone.trim() };
        } else if (method === "qr") {
            body = { method: "qr" };
        } else {
            body = { method: "password", username: username.trim(), password, ...(isLinkedIn ? { recruiter } : {}) };
        }
        signIn.mutate({ id: sessionId, body }, { onSuccess: () => onSubmitted() });
    };
    const left = attemptsRemaining(signIn.error);
    const edit = (fn: (v: string) => void) => (v: string) => {
        fn(v);
        onDirty();
    };

    if (!spec) {
        return (
            <div className="space-y-2.5">
                <div className="h-7 w-56 rounded-md skeleton-shimmer" />
                <div className="h-14 rounded-md skeleton-shimmer" />
            </div>
        );
    }
    if (methods.length === 0) return <Notice tone="error">{name} offers no sign-in Warmbly can use right now. Try again later.</Notice>;

    return (
        <form
            className="space-y-3"
            onSubmit={(e) => {
                e.preventDefault();
                submit();
            }}
        >
            {methods.length > 1 && (
                <div className="inline-flex flex-wrap rounded-md border border-slate-200 p-0.5 bg-slate-50" role="tablist" aria-label="How to sign in">
                    {methods.map((m) => {
                        const Icon = METHOD_ICON[m] ?? KeyRoundIcon;
                        return (
                            <button
                                key={m}
                                type="button"
                                role="tab"
                                aria-selected={method === m}
                                onClick={() => setMethod(m)}
                                className={cn(
                                    "h-6 px-2 rounded text-[11.5px] inline-flex items-center gap-1.5 transition-colors",
                                    method === m ? "bg-white text-slate-900 font-medium shadow-sm ring-1 ring-slate-200" : "text-slate-500 hover:text-slate-800",
                                )}
                            >
                                <Icon className="w-3 h-3" /> {m === "password" && isLinkedIn ? "Email and password" : AUTH_METHOD_LABEL[m]}
                            </button>
                        );
                    })}
                </div>
            )}

            {method === "cookie" && (
                <div className="space-y-2.5">
                    {[...required, ...optional].map((n, i) => (
                        <div key={n}>
                            <Label htmlFor={`social-cookie-${n}`}>
                                {n} cookie{required.includes(n) ? "" : isLinkedIn ? " (Sales Navigator or Recruiter, optional)" : " (optional)"}
                            </Label>
                            <TextInput
                                id={`social-cookie-${n}`}
                                value={cookies[n] ?? ""}
                                onChange={edit((v) => setCookies((c) => ({ ...c, [n]: v })))}
                                autoComplete="off"
                                className="w-full font-mono"
                                autoFocus={i === 0}
                            />
                        </div>
                    ))}
                    {required.length > 0 && (
                        <p className="text-[11px] text-slate-500 leading-relaxed">
                            In a browser signed in to {name}, open the developer tools, then Application, Cookies, and copy the value of {required.join(" and ")}.
                        </p>
                    )}
                </div>
            )}

            {method === "password" && (
                <div className="space-y-2.5">
                    <div>
                        <Label htmlFor="social-user">{user.label}</Label>
                        <TextInput id="social-user" value={username} onChange={edit(setUsername)} type={user.type} autoComplete="username" placeholder={user.placeholder} className="w-full" autoFocus />
                    </div>
                    <div>
                        <Label htmlFor="social-pass">Password</Label>
                        <TextInput id="social-pass" value={password} onChange={edit(setPassword)} type="password" autoComplete="current-password" className="w-full" />
                    </div>
                </div>
            )}

            {method === "qr" && (
                <Heading icon={QrCodeIcon} title={`Scan a code with ${name}`}>
                    Warmbly shows a QR code next. Open {name} on the phone, go to linked devices and scan it. Keep this window open until it moves on.
                </Heading>
            )}

            {method === "pairing_code" && (
                <div className="space-y-2.5">
                    <Heading icon={SmartphoneIcon} title={`Link with a code instead of scanning`}>
                        Enter the account&apos;s phone number with the country code. Warmbly shows a code to type into {name} under linked devices.
                    </Heading>
                    <div>
                        <Label htmlFor="social-phone">Phone number</Label>
                        <TextInput id="social-phone" value={phone} onChange={edit(setPhone)} type="tel" autoComplete="tel" placeholder="+44 7700 900123" className="w-full" maxLength={24} autoFocus />
                    </div>
                </div>
            )}

            {isLinkedIn && (method === "password" || method === "cookie") && (
                <div className="flex items-start gap-2.5 rounded-lg border border-slate-200 px-3 py-2.5">
                    <Toggle value={recruiter} onChange={setRecruiter} ariaLabel="Connect the Recruiter seat instead of Sales Navigator" />
                    <div className="min-w-0">
                        <p className="text-[12px] text-slate-800">Connect the Recruiter seat instead of Sales Navigator</p>
                        <p className="text-[11px] text-slate-500 leading-snug">LinkedIn allows one of the two on a connection.</p>
                    </div>
                </div>
            )}

            {(method === "password" || method === "cookie") && (
                <p className="flex items-start gap-1.5 text-[11px] text-slate-500 leading-relaxed">
                    <LockIcon className="w-3 h-3 mt-0.5 shrink-0" />
                    What you enter goes to {name} once to sign in. Warmbly does not keep your password or cookies.
                </p>
            )}

            {(signIn.isError || session?.checkpoint?.last_error) && (
                <Notice tone="error">
                    {signIn.isError ? socialErrorMessage(signIn.error) : session?.checkpoint?.last_error}
                    {left !== null && ` ${left === 1 ? "1 attempt" : `${left} attempts`} left.`}
                </Notice>
            )}

            <div className="flex justify-end">
                <button type="submit" disabled={!ready || signIn.isPending} className={primary}>
                    {signIn.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                    {method === "qr" ? "Show the QR code" : method === "pairing_code" ? "Get a pairing code" : "Sign in"}
                </button>
            </div>
        </form>
    );
}

const DELIVERY: Record<string, string> = {
    sms: "by text message",
    email: "by email",
    app: "in the app",
    authenticator: "in your authenticator app",
    totp: "in your authenticator app",
    whatsapp: "on WhatsApp",
    phone: "by phone",
};

const choiceId = (c: ConnectChoice | string) => (typeof c === "string" ? c : c.id);
const choiceLabel = (c: ConnectChoice | string) => (typeof c === "string" ? c.replace(/_/g, " ") : (c.label ?? c.name ?? c.id));
const choiceAttempts = (c: ConnectChoice | string) => (typeof c === "string" ? null : (c.attempts_remaining ?? null));

function ChoiceList({ choices, value, onChange, label }: { choices: (ConnectChoice | string)[]; value: string | null; onChange: (id: string) => void; label: string }) {
    return (
        <div role="radiogroup" aria-label={label} className="space-y-1.5">
            {choices.map((c) => {
                const id = choiceId(c);
                const left = choiceAttempts(c);
                return (
                    <button
                        key={id}
                        type="button"
                        role="radio"
                        aria-checked={value === id}
                        disabled={left === 0}
                        onClick={() => onChange(id)}
                        className={cn(
                            "w-full h-9 px-3 rounded-md border text-left text-[12.5px] flex items-center gap-2 transition-colors disabled:cursor-not-allowed disabled:opacity-50",
                            value === id ? "border-sky-400 bg-sky-50 text-sky-900" : "border-slate-200 hover:border-slate-300 text-slate-800",
                        )}
                    >
                        <span className={cn("size-3.5 rounded-full border flex items-center justify-center", value === id ? "border-sky-600" : "border-slate-300")}>
                            {value === id && <span className="size-1.5 rounded-full bg-sky-600" />}
                        </span>
                        <span className="min-w-0 flex-1 truncate">{choiceLabel(c)}</span>
                        {left !== null && <span className="shrink-0 text-[11px] text-slate-500">{left === 0 ? "No attempts left" : left === 1 ? "1 attempt left" : `${left} attempts left`}</span>}
                    </button>
                );
            })}
        </div>
    );
}

function CheckpointStep({
    sessionId,
    session,
    checkpoint,
    name,
    onSignInAgain,
}: {
    sessionId: string;
    session: ConnectSession;
    checkpoint: ConnectCheckpoint;
    name: string;
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
    const problem = (answer.isError || method.isError ? socialErrorMessage(err) : null) ?? checkpoint.last_error ?? null;
    const errorLine = problem ? (
        <Notice tone="error">
            {problem}
            {left !== null && left !== undefined && ` ${left === 1 ? "1 attempt" : `${left} attempts`} left.`}
        </Notice>
    ) : null;

    const choices = useMemo(() => checkpoint.choices ?? [], [checkpoint.choices]);

    if (type === "qr_code") {
        return (
            <div className="space-y-3">
                <Heading icon={QrCodeIcon} title={`Scan this code with ${name}`}>
                    On the phone, open {name}, go to linked devices, choose to link a device and point the camera at the code. It refreshes by itself.
                </Heading>
                <div className="flex justify-center">
                    <div className="rounded-xl border border-slate-200 bg-white p-3">
                        {checkpoint.qr_code ? (
                            <QRCodeSVG value={checkpoint.qr_code} size={196} marginSize={1} aria-label={`${name} sign-in QR code`} />
                        ) : (
                            <div className="size-[196px] rounded-md skeleton-shimmer" />
                        )}
                    </div>
                </div>
                <Waiting>Waiting for the scan</Waiting>
                {errorLine}
                <div className="flex justify-end">
                    <button type="button" onClick={onSignInAgain} className={secondary}>
                        Sign in another way
                    </button>
                </div>
            </div>
        );
    }

    if (type === "pairing_code") {
        return (
            <div className="space-y-3">
                <Heading icon={SmartphoneIcon} title={`Type this code into ${name}`}>
                    On the phone, open {name}, go to linked devices, choose to link a device, then link with a phone number instead and enter the code.
                </Heading>
                <div className="rounded-xl border border-slate-200 bg-slate-50/60 px-4 py-4 text-center">
                    {checkpoint.pairing_code ? (
                        <p className="font-mono text-[28px] font-semibold tracking-[0.18em] text-slate-900 select-all" aria-live="polite">
                            {checkpoint.pairing_code}
                        </p>
                    ) : (
                        <span className="inline-block h-8 w-48 rounded skeleton-shimmer" />
                    )}
                </div>
                <Waiting>Waiting for the code to be entered</Waiting>
                {errorLine}
                <div className="flex justify-end">
                    <button type="button" onClick={onSignInAgain} className={secondary}>
                        Use another number
                    </button>
                </div>
            </div>
        );
    }

    if (type === "loading") {
        return (
            <div className="space-y-3">
                <Heading icon={SmartphoneIcon} title={`${name} is getting ready`}>
                    {name} is preparing the linked session. This takes a few seconds and moves on by itself.
                </Heading>
                <Waiting>Preparing</Waiting>
                {errorLine}
            </div>
        );
    }

    if (type === "encrypted_history") {
        const ok = !!choice && code.length > 0;
        return (
            <form
                className="space-y-3"
                onSubmit={(e) => {
                    e.preventDefault();
                    if (ok && choice) answer.mutate({ id: sessionId, body: { choice, secret: code } });
                }}
            >
                <Heading icon={HistoryIcon} title={`Unlock the ${name} chat history`}>
                    {name} keeps end-to-end encrypted chats behind a PIN or a recovery code. Pick how to unlock them and enter it, so replies show up in Warmbly.
                </Heading>
                <ChoiceList choices={choices} value={choice} onChange={setChoice} label="How to unlock" />
                {choices.length === 0 && <p className="text-[12px] text-slate-500">{name} offered no way to unlock the history. Sign in again.</p>}
                <div>
                    <Label htmlFor="social-secret">PIN or recovery code</Label>
                    <TextInput id="social-secret" value={code} onChange={setCode} type="password" autoComplete="off" className="w-full" />
                </div>
                {errorLine}
                <div className="flex items-center justify-end gap-2">
                    {choices.length === 0 && (
                        <button type="button" onClick={onSignInAgain} className={secondary}>
                            Sign in again
                        </button>
                    )}
                    <button type="submit" disabled={!ok || answer.isPending} className={primary}>
                        {answer.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        Unlock
                    </button>
                </div>
            </form>
        );
    }

    if (type === "two_factor" && checkpoint.delivery === "cloud_password") {
        return (
            <form
                className="space-y-3"
                onSubmit={(e) => {
                    e.preventDefault();
                    if (code) answer.mutate({ id: sessionId, body: { two_factor_password: code } });
                }}
            >
                <Heading icon={LockIcon} title={`Enter the ${name} cloud password`}>
                    This account has two-step verification on. Enter the password set for it in {name}&apos;s privacy settings.
                </Heading>
                <div>
                    <Label htmlFor="social-cloud-password">{name} cloud password</Label>
                    <TextInput id="social-cloud-password" value={code} onChange={setCode} type="password" autoComplete="current-password" className="w-full" autoFocus />
                </div>
                {errorLine}
                <div className="flex justify-end">
                    <button type="submit" disabled={!code || answer.isPending} className={primary}>
                        {answer.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        Verify
                    </button>
                </div>
            </form>
        );
    }

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
                <Heading icon={isPhone ? SmartphoneIcon : KeyRoundIcon} title={isPhone ? `${name} wants a phone number` : `Enter the code ${name} sent`}>
                    {isPhone
                        ? `${name} asks for a phone number to send a code to. Use one you can read now, with the country code.`
                        : where
                          ? `${name} sent a code ${where}.`
                          : `${name} sent a verification code to the account's owner.`}
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
                {type === "otp_or_app" && <Waiting>Or approve the sign-in in the {name} app on your phone. This moves on by itself.</Waiting>}
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
                <Heading icon={SmartphoneIcon} title={`Approve the sign-in in the ${name} app`}>
                    {name} sent a prompt to the {name} app on the account owner&apos;s phone. Tap Yes there; this window moves on by itself.
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
                <Heading icon={isMethod ? KeyRoundIcon : LayersIcon} title={isMethod ? `How should ${name} verify it is you?` : "Which contract should this account use?"}>
                    {isMethod ? `Pick where ${name} sends the verification code.` : "This account belongs to more than one Sales Navigator or Recruiter contract."}
                </Heading>
                <ChoiceList choices={choices} value={choice} onChange={setChoice} label={isMethod ? "Verification method" : "Contract"} />
                {choices.length === 0 && <p className="text-[12px] text-slate-500">{name} offered no choices. Sign in again.</p>}
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
                <Heading icon={ShieldAlertIcon} title={`${name} wants a captcha`}>
                    {image ? "Type the characters in the picture." : `${name} asked for a puzzle that cannot be shown here. Sign in with a session cookie instead, which skips it.`}
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
            <Heading icon={AlertCircleIcon} title={`${name} asked for a check Warmbly cannot complete`}>
                Sign in once in the {name} app or a browser to clear it, or sign in another way.
            </Heading>
            <div className="flex justify-end">
                <button type="button" onClick={onSignInAgain} className={secondary}>
                    Sign in again
                </button>
            </div>
        </div>
    );
}

function DoneStep({ account, reconnect, name }: { account: SocialAccount | null; reconnect: boolean; name: string }) {
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
                <p className="mt-2 text-[13.5px] font-semibold text-slate-900">{reconnect ? "Reconnected" : `${name} connected`}</p>
                <p className="mt-0.5 text-[12px] text-slate-500 max-w-[40ch]">It starts gently and works within its daily limits and working hours. Change them in the account&apos;s settings.</p>
            </div>
            {account && (
                <div className="rounded-lg border border-slate-200 px-3 py-2.5 flex items-center gap-3">
                    <Avatar src={account.avatar_url} name={account.display_name} size={36} />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12.5px] font-medium text-slate-900 truncate">{account.display_name}</p>
                        {(account.headline || account.username) && <p className="text-[11.5px] text-slate-500 truncate">{account.headline || account.username}</p>}
                        <LicenceChips licences={account.licences} className="mt-1" />
                    </div>
                </div>
            )}
        </div>
    );
}

function FailedStep({ session, charged, name }: { session: ConnectSession | undefined; charged: number; name: string }) {
    const code = session?.error?.code ?? session?.status;
    const message =
        session?.status === "expired"
            ? "This sign-in timed out. Start again when you are ready."
            : session?.status === "cancelled"
              ? "This sign-in was stopped."
              : code === "sign_in_failed"
                ? `${name} did not accept the sign-in.`
                : code === "too_many_failed_sign_ins"
                  ? "Too many refused sign-ins today for this account. Try again tomorrow."
                  : (session?.error?.message ?? `${name} did not finish the sign-in.`);
    return (
        <div className="space-y-3">
            <Heading icon={AlertCircleIcon} title="The account did not connect">
                {message}
            </Heading>
            {charged > 0 && <Notice tone="info">The {fmtMicros(charged)} charged for this account goes back to your balance when a sign-in does not finish.</Notice>}
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

