// "Senders": who the mailboxes send as, set once for the whole order: a name and a photo each. One line under them says
// what every domain gets; Customize opens the addresses, provider, count and signature, which all have working defaults.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { CameraIcon, ChevronDownIcon, Loader2Icon, PlusIcon, SlidersHorizontalIcon, XIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import type { StoreProvider } from "@/lib/api/models/app/store/Store";
import { useSeedStorePhoto } from "@/lib/api/hooks/app/store/useStore";
import {
    autoPerSender,
    canAddSender,
    effectivePatterns,
    effectivePerSender,
    MAX_SENDERS,
    perSenderClamped,
    perSenderMax,
    PROVIDER_NAMES,
    PROVIDER_SHORT,
    senderProblems,
    sendersSummary,
    type Cart,
    type CartAction,
    type CartLimits,
    type Sender,
} from "@/lib/buy/cart";
import { CARD_SPRING, EASE, PRESS_SPRING } from "@/lib/buy/motion";
import { defaultPatterns } from "@/lib/buy/patterns";
import { cn } from "@/lib/utils";
import { CountStepper, Segmented, SenderAvatar } from "./Controls";
import PatternPicker from "./PatternPicker";
import PhotoPicker from "./PhotoPicker";
import StoreSignature from "./StoreSignature";

const SAMPLE = { first: "Ada", last: "Lovelace" };
/** Whether the buyer left Customize open, remembered on this browser only. */
export const CUSTOMIZE_KEY = "warmbly.store.customize";

function readOpen(): boolean {
    try {
        return window.localStorage.getItem(CUSTOMIZE_KEY) === "1";
    } catch {
        return false;
    }
}

function writeOpen(open: boolean) {
    try {
        window.localStorage.setItem(CUSTOMIZE_KEY, open ? "1" : "0");
    } catch {
        // Storage blocked: it stays as chosen for this page.
    }
}

export default function Senders({
    cart,
    dispatch,
    limits,
    providers,
    photos,
    profileUrl,
    nudged,
}: {
    cart: Cart;
    dispatch: React.Dispatch<CartAction>;
    limits: CartLimits;
    providers: StoreProvider[];
    /** Whether profile photos can be uploaded on this instance. */
    photos: boolean;
    /** The account's own photo, previewed on the first sender until it is copied for the order. */
    profileUrl: string | null;
    /** Pay was pressed while blocked, so empty names are marked too. */
    nudged: boolean;
}) {
    const reduced = useReducedMotion();
    const [focusId, setFocusId] = React.useState<string | null>(null);
    const [open, setOpen] = React.useState(readOpen);
    const addable = canAddSender(cart);
    const each = effectivePerSender(cart, limits);
    const perDomain = cart.senders.length * each;
    const clamped = perSenderClamped(cart, limits);
    const named = cart.senders.find((s) => s.first.trim() && s.last.trim());
    const sample = named ?? SAMPLE;
    const patterns = effectivePatterns(cart);
    const addHint = addable ? undefined : `Up to ${MAX_SENDERS} senders.`;
    const toggle = () => {
        setOpen(!open);
        writeOpen(!open);
    };

    return (
        <section aria-labelledby="buy-senders" className="rounded-xl border border-slate-200 bg-white">
            <header className="px-3.5 pt-3 pb-2 flex items-center gap-2">
                <h2 id="buy-senders" className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Senders
                </h2>
                <span className="text-[11px] text-slate-400 truncate">Set once, used on every domain</span>
                <motion.button
                    type="button"
                    onClick={() => {
                        if (!addable) return;
                        setFocusId(`s${cart.senders.reduce((m, s) => Math.max(m, Number(s.id.replace(/^s/, "")) || 0), 0) + 1}`);
                        dispatch({ type: "sender_add" });
                    }}
                    aria-disabled={!addable || undefined}
                    title={addHint}
                    whileTap={reduced || !addable ? undefined : { scale: 0.95 }}
                    transition={PRESS_SPRING}
                    className={cn(
                        "ml-auto h-7 px-2.5 rounded-md border inline-flex items-center gap-1.5 text-[12px] font-medium shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                        addable ? "border-slate-200 bg-white text-slate-700 hover:border-slate-300 hover:text-slate-900" : "border-slate-200 bg-slate-50 text-slate-400 cursor-not-allowed",
                    )}
                >
                    <PlusIcon className="w-3.5 h-3.5" /> Add sender
                </motion.button>
            </header>

            <ul className="px-3.5 grid gap-2 xl:grid-cols-2">
                <AnimatePresence initial={false}>
                    {cart.senders.map((s, i) => (
                        <motion.li
                            key={s.id}
                            layout={reduced ? false : "position"}
                            initial={reduced ? { opacity: 0 } : { opacity: 0, y: -6, scale: 0.98 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.97, transition: { duration: 0.14 } }}
                            transition={reduced ? { duration: 0.15 } : CARD_SPRING}
                        >
                            <SenderRow
                                s={s}
                                index={i}
                                removable={cart.senders.length > 1}
                                dispatch={dispatch}
                                photos={photos}
                                fallbackUrl={i === 0 && s.photo === undefined ? profileUrl : null}
                                autoFocus={focusId === s.id}
                                nudged={nudged}
                            />
                        </motion.li>
                    ))}
                </AnimatePresence>
            </ul>
            {addHint && <p className="px-3.5 pt-1.5 text-[11px] text-slate-400">{addHint}</p>}

            <div className="mt-3 px-3.5 py-2 border-t border-slate-100 flex items-center gap-2 min-w-0">
                <p className="min-w-0 flex-1 text-[11.5px] text-slate-500 leading-snug">
                    {sendersSummary(cart, limits)}
                </p>
                <button
                    type="button"
                    onClick={toggle}
                    aria-expanded={open}
                    aria-controls="buy-customize"
                    className={cn(
                        "h-7 px-2 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                        open ? "text-slate-900 bg-slate-100" : "text-slate-600 hover:text-slate-900 hover:bg-slate-100",
                    )}
                >
                    <SlidersHorizontalIcon className="w-3.5 h-3.5 text-slate-400" />
                    Customize
                    <ChevronDownIcon className={cn("w-3 h-3 text-slate-400 transition-transform", open && "rotate-180")} />
                </button>
            </div>
            <AnimatePresence initial={false}>
                {clamped !== null && (
                    <motion.p
                        key="clamped"
                        role="status"
                        initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                        transition={{ duration: 0.2, ease: EASE }}
                        className="overflow-hidden px-3.5 pb-2 text-[11px] text-amber-800 leading-snug"
                    >
                        Lowered from {clamped} to {each} per sender, so each domain stays within {limits.max} mailboxes. Your {clamped} comes back with fewer senders.
                    </motion.p>
                )}
            </AnimatePresence>
            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        id="buy-customize"
                        role="group"
                        aria-label="Customize the mailboxes"
                        // Clips only while it slides; once open, the address dropdown inside may overhang it.
                        initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0, overflow: "hidden" }}
                        animate={{ opacity: 1, height: "auto", transitionEnd: { overflow: "visible" } }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0, overflow: "hidden" }}
                        transition={{ duration: 0.2, ease: EASE }}
                    >
                        <div className="px-3.5 py-3 border-t border-slate-100 grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] xl:grid-cols-[minmax(0,1fr)_auto_auto] items-end">
                            <div className="min-w-0">
                                <span className="flex items-baseline gap-1.5 mb-1 min-w-0">
                                    <span className="text-[11px] text-slate-500 shrink-0">Addresses</span>
                                    <span className="text-[11px] text-slate-400 leading-snug">Patterns turn each name into an address, used in this order</span>
                                </span>
                                <PatternPicker
                                    value={patterns}
                                    automatic={cart.patterns === null}
                                    sample={sample}
                                    suggested={defaultPatterns(cart.senders.length)}
                                    onChange={(next) => dispatch({ type: "patterns", patterns: next })}
                                    onAuto={() => dispatch({ type: "patterns_auto" })}
                                />
                            </div>
                            <div>
                                <span className="block mb-1 text-[11px] text-slate-500">Provider</span>
                                <Segmented
                                    label="Mail provider for the order"
                                    pillId="buy-provider-pill"
                                    value={cart.provider}
                                    onChange={(provider) => dispatch({ type: "default_provider", provider })}
                                    options={providers.map((p) => ({
                                        value: p,
                                        name: PROVIDER_NAMES[p],
                                        label: (
                                            <>
                                                <ProviderLogo id={p} size="xs" framed={false} />
                                                {PROVIDER_SHORT[p]}
                                            </>
                                        ),
                                    }))}
                                />
                            </div>
                            <div className="flex items-center gap-2 md:col-span-2 xl:col-span-1">
                                <div>
                                    <span className="block mb-1 text-[11px] text-slate-500">Per sender</span>
                                    <CountStepper
                                        value={each}
                                        min={1}
                                        max={perSenderMax(cart, limits)}
                                        onChange={(n) => dispatch({ type: "per_sender", count: n, limits })}
                                        label="Mailboxes per sender on each domain"
                                        unit="mailbox per sender"
                                    />
                                </div>
                                <p className="self-end pb-1.5 text-[11px] text-slate-500 leading-snug">
                                    <span className="font-medium text-slate-700 tabular-nums">{perDomain}</span> per domain
                                    {cart.perSender === null ? (
                                        <span className="block text-emerald-700">Suggested for {cart.senders.length === 1 ? "one sender" : `${cart.senders.length} senders`}</span>
                                    ) : (
                                        <button
                                            type="button"
                                            onClick={() => dispatch({ type: "per_sender_auto" })}
                                            className="block rounded text-sky-700 hover:text-sky-900 font-medium outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                                        >
                                            Use the suggested {autoPerSender(cart.senders.length, limits.max)}
                                        </button>
                                    )}
                                </p>
                            </div>
                        </div>
                        <StoreSignature cart={cart} dispatch={dispatch} />
                    </motion.div>
                )}
            </AnimatePresence>
        </section>
    );
}

function SenderRow({
    s,
    index,
    removable,
    dispatch,
    photos,
    fallbackUrl,
    autoFocus,
    nudged,
}: {
    s: Sender;
    index: number;
    removable: boolean;
    dispatch: React.Dispatch<CartAction>;
    photos: boolean;
    fallbackUrl: string | null;
    autoFocus: boolean;
    nudged: boolean;
}) {
    const problems = senderProblems(s);
    const [touched, setTouched] = React.useState({ first: false, last: false });
    // An empty name is marked once it was left or Pay was pressed; anything typed that cannot be used, at once.
    const show = (f: "first" | "last") => (s[f].trim() || touched[f] || nudged ? problems[f] : null);
    const id = `buy-sender-${s.id}`;
    return (
        <div className="flex items-start gap-2.5 min-w-0">
            {photos ? (
                <SenderPhoto s={s} index={index} dispatch={dispatch} fallbackUrl={fallbackUrl} />
            ) : (
                <SenderAvatar photoId={null} first={s.first} last={s.last} tone={index} className="size-8 mt-px text-[11px]" />
            )}
            <div className="min-w-0 flex-1 grid grid-cols-2 gap-2">
                <NameField id={`${id}-first`} label={`Sender ${index + 1} first name`} placeholder="First name" value={s.first} error={show("first")} autoFocus={autoFocus} autoComplete={index === 0 ? "given-name" : "off"}
                    onChange={(v) => dispatch({ type: "sender_name", id: s.id, first: v })} onBlur={() => setTouched((t) => ({ ...t, first: true }))} />
                <NameField id={`${id}-last`} label={`Sender ${index + 1} last name`} placeholder="Last name" value={s.last} error={show("last")} autoComplete={index === 0 ? "family-name" : "off"}
                    onChange={(v) => dispatch({ type: "sender_name", id: s.id, last: v })} onBlur={() => setTouched((t) => ({ ...t, last: true }))} />
            </div>
            {removable && (
                <button
                    type="button"
                    onClick={() => dispatch({ type: "sender_remove", id: s.id })}
                    aria-label={`Remove sender ${[s.first, s.last].filter(Boolean).join(" ") || index + 1}`}
                    className="mt-0.5 size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200 shrink-0"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            )}
        </div>
    );
}

function NameField({
    id,
    label,
    placeholder,
    value,
    error,
    autoFocus,
    autoComplete,
    onChange,
    onBlur,
}: {
    id: string;
    label: string;
    placeholder: string;
    value: string;
    error: string | null;
    autoFocus?: boolean;
    autoComplete: string;
    onChange: (v: string) => void;
    onBlur: () => void;
}) {
    return (
        <div className="min-w-0">
            <label htmlFor={id} className="sr-only">
                {label}
            </label>
            <input
                id={id}
                value={value}
                onChange={(e) => onChange(e.target.value)}
                onBlur={onBlur}
                placeholder={placeholder}
                autoFocus={autoFocus}
                autoComplete={autoComplete}
                aria-invalid={!!error || undefined}
                aria-describedby={error ? `${id}-err` : undefined}
                className={cn(
                    "w-full h-8 px-2.5 rounded-md border bg-white text-[16px] md:text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:ring-2 transition-colors",
                    error ? "border-red-300 focus:border-red-400 focus:ring-red-100" : "border-slate-200 focus:border-sky-400 focus:ring-sky-100",
                )}
            />
            <AnimatePresence initial={false}>
                {error && (
                    <motion.p
                        id={`${id}-err`}
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.16, ease: EASE }}
                        className="overflow-hidden text-[11px] text-red-600 leading-snug pt-0.5"
                    >
                        {error}
                    </motion.p>
                )}
            </AnimatePresence>
        </div>
    );
}

/** The sender's photo: a round button that shows it and opens the picker; the one upload goes on all their mailboxes. */
function SenderPhoto({ s, index, dispatch, fallbackUrl }: { s: Sender; index: number; dispatch: React.Dispatch<CartAction>; fallbackUrl: string | null }) {
    const seed = useSeedStorePhoto();
    const name = [s.first, s.last].filter(Boolean).join(" ") || `sender ${index + 1}`;
    const has = !!s.photo || !!fallbackUrl;
    return (
        <span className="relative shrink-0 inline-flex mt-px">
            <PhotoPicker
                hint={`Every mailbox ${s.first.trim() || "this sender"} sends from gets this photo. Recipients see it next to the sender name in Gmail and Outlook.`}
                onPicked={({ photo, blob }) => {
                    seed(photo.id, blob);
                    dispatch({ type: "sender_photo", id: s.id, photo: { id: photo.id, at: Date.now() } });
                }}
            >
                {(open, busy) => (
                    <button
                        type="button"
                        onClick={open}
                        disabled={busy}
                        aria-label={has ? `Change the photo for ${name}` : `Add a photo for ${name}`}
                        title={has ? "Change the photo" : "Add a photo"}
                        className="group relative size-8 rounded-full outline-none focus-visible:ring-2 focus-visible:ring-sky-200 focus-visible:ring-offset-1"
                    >
                        {has ? (
                            <SenderAvatar photoId={s.photo?.id} fallbackUrl={fallbackUrl} first={s.first} last={s.last} tone={index} className="size-8 text-[11px] ring-1 ring-slate-200" />
                        ) : (
                            <span className="size-8 rounded-full border border-dashed border-slate-300 bg-slate-50 text-slate-400 group-hover:border-slate-400 group-hover:text-slate-600 inline-flex items-center justify-center transition-colors">
                                <CameraIcon className="w-3.5 h-3.5" />
                            </span>
                        )}
                        {busy ? (
                            <span className="absolute inset-0 rounded-full bg-white/70 inline-flex items-center justify-center">
                                <Loader2Icon className="w-3 h-3 animate-spin text-slate-500" />
                            </span>
                        ) : (
                            has && (
                                <span className="absolute inset-0 rounded-full bg-slate-900/40 text-white inline-flex items-center justify-center opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 transition-opacity">
                                    <CameraIcon className="w-3 h-3" />
                                </span>
                            )
                        )}
                    </button>
                )}
            </PhotoPicker>
            {has && (
                <button
                    type="button"
                    onClick={() => dispatch({ type: "sender_photo", id: s.id, photo: null })}
                    aria-label={`Remove the photo for ${name}`}
                    title="No photo"
                    className="absolute -top-1 -right-1 size-3.5 rounded-full bg-white border border-slate-200 text-slate-400 hover:text-slate-800 inline-flex items-center justify-center outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                >
                    <XIcon className="w-2 h-2" />
                </button>
            )}
        </span>
    );
}
