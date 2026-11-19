// The mailbox drawer's Subscription card, for a mailbox bought through Warmbly: price, paid-through, cancel or resume.
import React from "react";
import toast from "react-hot-toast/headless";
import { AnimatePresence } from "framer-motion";
import { Link } from "@tanstack/react-router";
import { CalendarClockIcon, CalendarXIcon, CameraIcon, KeyRoundIcon, Loader2Icon, PauseCircleIcon, RotateCcwIcon, ShieldCheckIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import { signInStoreMailbox } from "@/lib/api/client/app/store/store";
import { useResumeStoreMailbox, useSetStoreMailboxPhoto, useStoreStatus } from "@/lib/api/hooks/app/store/useStore";
import type { MailboxSignIn, StoreMailbox } from "@/lib/api/models/app/store/Store";
import { PROVIDER_NAMES } from "@/lib/buy/cart";
import { fmtMonthly } from "@/lib/buy/money";
import { storeErrorMessage } from "@/lib/buy/errors";
import { shortDate as day, storeMailboxEnded } from "@/lib/buy/purchased";
import PhotoPicker from "./PhotoPicker";
import { PressButton } from "./Press";
import SignInDialog from "./SignInDialog";
import StorePaused from "./StorePaused";
import { useCancelAtPeriodEnd, usePurchasedMailbox, useStorePausedStatus } from "./usePurchasedMailbox";
import { OrdersLink, StoreChargesLink } from "./StoreLinks";

/** For a mailbox whose read says purchased; renders nothing until its store row is found. */
export default function SubscriptionCard({ emailAccountId }: { emailAccountId: string }) {
    const m = usePurchasedMailbox(emailAccountId);
    if (!m) return null;
    return <Card m={m} />;
}

function Card({ m }: { m: StoreMailbox }) {
    const confirm = useConfirm();
    // Every mailbox is charged together, a few days before the workspace's billing day.
    const status = useStoreStatus().data;
    const nextCharge = day(status?.next_bundle?.charge_on ?? null);
    const canBill = usePermission("MANAGE_BILLING");
    const cancel = useCancelAtPeriodEnd();
    const resume = useResumeStoreMailbox();
    const paid = day(m.paid_through);
    const ends = day(m.cancels_at);
    const ended = storeMailboxEnded(m.status);
    const endedOn = day(m.cancels_at ?? m.cancelled_at);
    const busy = cancel.pending || resume.isPending;
    // While GraphKit is disconnected nothing here can reach the provider, so only the notice and the history links stay.
    const paused = useStorePausedStatus();
    const live = !ended && !paused;
    // A fresh sign-in each time, held only while its dialog is open; never in the query cache.
    const [signIn, setSignIn] = React.useState<MailboxSignIn | null>(null);
    const [signingIn, setSigningIn] = React.useState(false);
    const openSignIn = async () => {
        if (signingIn) return;
        setSigningIn(true);
        try {
            setSignIn(await signInStoreMailbox(m.id));
        } catch (e) {
            toast.error(storeErrorMessage(e));
        } finally {
            setSigningIn(false);
        }
    };

    const askResume = () =>
        confirm.show(
            `Keep ${m.email}? It renews for ${fmtMonthly(m.monthly_micros)} on ${ends ?? "its renewal date"} as before.`,
            async () => {
                try {
                    await resume.mutateAsync(m.id);
                    toast.success(`${m.email} will keep renewing`);
                } catch (e) {
                    toast.error(storeErrorMessage(e));
                }
            },
            undefined,
            { title: "Keep mailbox", confirmLabel: "Keep it", tone: "primary" },
        );

    return (
        <div className="px-5 py-5 space-y-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Subscription</div>
            <div className="flex items-start gap-3">
                <ProviderLogo id={m.provider} size="lg" />
                <div className="min-w-0 flex-1">
                    <div className="text-[12.5px] font-medium text-slate-900 flex flex-wrap items-center gap-1.5">
                        {PROVIDER_NAMES[m.provider] ?? "Mailbox"} · {fmtMonthly(m.monthly_micros)}
                        {m.is_admin && (
                            <span
                                title={`Manages the ${PROVIDER_NAMES[m.provider] ?? "mail"} account for this domain`}
                                className="h-[18px] px-1.5 rounded-full bg-sky-50 border border-sky-100 text-sky-700 text-[10.5px] font-medium inline-flex items-center gap-1"
                            >
                                <ShieldCheckIcon className="w-2.5 h-2.5" /> Admin
                            </span>
                        )}
                    </div>
                    <div className="text-[11px] text-slate-500">
                        {ended
                            ? "Bought through Warmbly. No longer billed."
                            : `Bought through Warmbly. ${paid ? `Paid through ${paid}. ` : ""}${nextCharge && !m.cancels_at ? `Next charge ${nextCharge} with your other mailboxes.` : "Billed monthly with your other mailboxes."}`}
                    </div>
                </div>
            </div>
            {m.paused && !ended && (
                <div className="rounded-md border border-amber-200 bg-amber-50/60 px-3 py-2.5 flex items-start gap-2">
                    <PauseCircleIcon className="w-3.5 h-3.5 text-amber-600 mt-0.5 shrink-0" />
                    <div className="min-w-0 flex-1 text-[11.5px] text-amber-900 leading-relaxed">
                        Campaign sending and warmup are paused because the workspace has no plan. The mailbox keeps working for reading and replying, and stays
                        billed at {fmtMonthly(m.monthly_micros)}.{" "}
                        <Link to="/app/settings/billing/{-$tab}" params={{ tab: "plans" }} className="font-medium text-amber-900 underline underline-offset-2">
                            Choose a plan
                        </Link>
                    </div>
                </div>
            )}
            {paused && !ended && <StorePaused status={paused} compact />}
            {m.is_admin && live && (
                <div className="flex flex-wrap items-center gap-2">
                    <PressButton tone="secondary" disabled={signingIn} onClick={() => void openSignIn()}>
                        {signingIn ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <KeyRoundIcon className="w-3 h-3" />} Admin sign-in
                    </PressButton>
                    <span className="text-[11px] text-slate-400">For the {PROVIDER_NAMES[m.provider] ?? "mail"} admin console of this domain.</span>
                </div>
            )}
            {status?.photos && live && <PhotoRow m={m} />}
            <AnimatePresence>{signIn && <SignInDialog signIn={signIn} admin={m.is_admin} onClose={() => setSignIn(null)} />}</AnimatePresence>
            {ended ? (
                <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 flex items-start gap-2">
                    <CalendarXIcon className="w-3.5 h-3.5 text-slate-500 mt-0.5 shrink-0" />
                    <span className="min-w-0 flex-1 text-[11.5px] text-slate-700 leading-relaxed">
                        Cancelled{endedOn ? ` on ${endedOn}` : ""}. The mailbox no longer exists, so it is switched off here. Its history stays until you
                        disconnect it below.
                    </span>
                </div>
            ) : paused ? null : ends ? (
                <div className="rounded-md border border-amber-200 bg-amber-50/60 px-3 py-2 flex flex-wrap items-center gap-2">
                    <CalendarClockIcon className="w-3.5 h-3.5 text-amber-600 shrink-0" />
                    <span className="min-w-0 flex-1 text-[11.5px] text-amber-900">Ends {ends}. It keeps working until then.</span>
                    <PressButton tone="secondary" disabled={!canBill || busy} onClick={askResume}>
                        <RotateCcwIcon className="w-3 h-3" /> Keep it
                    </PressButton>
                </div>
            ) : (
                <PressButton tone="secondary" disabled={!canBill || busy} onClick={() => cancel.ask(m)} className="text-red-700 hover:text-red-800">
                    Cancel at period end…
                </PressButton>
            )}
            {!canBill && live && <p className="text-[11px] text-slate-400">Changing the subscription needs the Manage billing permission.</p>}
            <div className="-ml-2 flex flex-wrap items-center gap-1">
                <OrdersLink />
                <StoreChargesLink />
            </div>
        </div>
    );
}

const PHOTO_STATE: Record<string, string> = {
    set: "Recipients see it next to the sender name.",
    pending: "On its way to the mailbox. It can take a few minutes to show.",
    none: "No photo yet. Recipients see the sender's initial.",
};

/** The mailbox's profile photo at Google or Microsoft. GraphKit sets photos but cannot remove one, so there is no Remove. */
function PhotoRow({ m }: { m: StoreMailbox }) {
    const set = useSetStoreMailboxPhoto();
    const provider = PROVIDER_NAMES[m.provider] ?? "the provider";
    return (
        <div className="flex flex-wrap items-center gap-2">
            <PhotoPicker
                hint={`${m.email} shows this photo in ${provider}. Recipients see it next to the sender name.`}
                onPicked={async ({ photo }) => {
                    const res = await set.mutateAsync({ ids: [m.id], photoId: photo.id });
                    if (res.failed > 0) throw res.error;
                    toast.success(res.done[0]?.photo === "set" ? "Profile photo updated" : "Profile photo on its way");
                }}
            >
                {(open, busy) => (
                    <PressButton tone="secondary" disabled={busy || set.isPending} onClick={open}>
                        {busy || set.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CameraIcon className="w-3 h-3" />}{" "}
                        {m.photo && m.photo !== "none" ? "Change photo" : "Add photo"}
                    </PressButton>
                )}
            </PhotoPicker>
            <span className="text-[11px] text-slate-400">{PHOTO_STATE[m.photo ?? "none"] ?? PHOTO_STATE.none}</span>
        </div>
    );
}
