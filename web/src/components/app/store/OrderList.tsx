// "Your order": one compact card per domain. Its mailboxes come from the senders (one each, named after them, with
// their photo); "Customize" overrides this domain only, and anything left untyped there still follows the senders.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { AlertCircleIcon, ArrowUpRightIcon, CheckIcon, ChevronDownIcon, RotateCcwIcon, ShieldCheckIcon, SlidersHorizontalIcon, XIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { FieldError, TextInput } from "@/components/ui/field";
import type { StoreProvider } from "@/lib/api/models/app/store/Store";
import { forwardingProblem, localPartMessage, normalizeForwarding } from "@/lib/buy/addresses";
import {
    adminIndex,
    collisionNote,
    domainIssue,
    domainMailboxes,
    domainProblems,
    effectivePerSender,
    effectiveProvider,
    PROVIDER_NAMES,
    PROVIDER_SHORT,
    type Cart,
    type CartAction,
    type CartDomain,
    type CartLimits,
    type CartMailbox,
} from "@/lib/buy/cart";
import { CARD_SPRING, EASE, PRESS_SPRING } from "@/lib/buy/motion";
import { cn } from "@/lib/utils";
import { CountStepper, Segmented, SenderAvatar } from "./Controls";

export default function OrderList({
    cart,
    dispatch,
    limits,
    providers,
}: {
    cart: Cart;
    dispatch: React.Dispatch<CartAction>;
    limits: CartLimits;
    providers: StoreProvider[];
}) {
    const reduced = useReducedMotion();
    const newCount = cart.domains.filter((d) => d.kind === "new").length;
    const boxes = cart.domains.reduce((n, d) => n + domainMailboxes(cart, d).length, 0);
    return (
        <section aria-label="Your order">
            <div className="flex items-baseline gap-2">
                <h2 className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Your order</h2>
                {cart.domains.length > 0 && (
                    <span className="text-[11px] text-slate-400 tabular-nums">
                        {cart.domains.length} {cart.domains.length === 1 ? "domain" : "domains"} · {boxes} {boxes === 1 ? "mailbox" : "mailboxes"}
                    </span>
                )}
            </div>
            <ul className="mt-2 grid gap-2.5 2xl:grid-cols-2 items-start">
                <AnimatePresence initial={false} mode="popLayout">
                    {cart.domains.map((d) => (
                        <motion.li
                            key={d.domain}
                            layout={reduced ? false : "position"}
                            initial={reduced ? { opacity: 0 } : { opacity: 0, x: 24, scale: 0.97 }}
                            animate={{ opacity: 1, x: 0, scale: 1 }}
                            exit={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.95, transition: { duration: 0.16, ease: EASE } }}
                            transition={reduced ? { duration: 0.15 } : CARD_SPRING}
                            className="min-w-0"
                        >
                            <DomainCard cart={cart} d={d} dispatch={dispatch} limits={limits} providers={providers} sharedForward={newCount > 1} />
                        </motion.li>
                    ))}
                </AnimatePresence>
            </ul>
            <AnimatePresence initial={false}>
                {cart.domains.length === 0 && (
                    <motion.p
                        key="empty"
                        role="status"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        className="rounded-xl border border-dashed border-slate-200 px-4 py-5 text-center text-[12px] text-slate-500"
                    >
                        {emptyCopy(cart)}
                    </motion.p>
                )}
            </AnimatePresence>
        </section>
    );
}

/** What each new domain will get, in the words of the current senders. */
function emptyCopy(cart: Cart): string {
    const each = effectivePerSender(cart);
    if (cart.senders.length === 1) {
        const who = cart.senders[0].first.trim() || "you";
        return each === 1
            ? `Add a domain from the search. Each one gets a mailbox for ${who}.`
            : `Add a domain from the search. Each one gets ${each} mailboxes for ${who}, each in a different address pattern.`;
    }
    const per = each === 1 ? "one mailbox" : `${each} mailboxes`;
    return `Add a domain from the search. Each one gets ${per} for each of your ${cart.senders.length} senders, named after them, with their photo.`;
}

function DomainCard({
    cart,
    d,
    dispatch,
    limits,
    providers,
    sharedForward,
}: {
    cart: Cart;
    d: CartDomain;
    dispatch: React.Dispatch<CartAction>;
    limits: CartLimits;
    providers: StoreProvider[];
    sharedForward: boolean;
}) {
    const boxes = domainMailboxes(cart, d);
    const note = collisionNote(boxes, d.domain);
    const problems = domainProblems(cart, d);
    const issue = domainIssue(cart, d);
    const admin = adminIndex(cart, d);
    const [editing, setEditing] = React.useState(false);
    // A typed address that went wrong keeps its editor open until it is fixed.
    const open = editing || !!issue;
    const editorId = `buy-edit-${d.domain}`;
    const provider = effectiveProvider(cart, d);
    const options = d.providerFixed ? [provider] : providers;
    return (
        <article aria-label={d.domain} className={cn("rounded-xl border bg-white transition-colors", issue ? "border-amber-200" : "border-slate-200 hover:border-slate-300")}>
            <header className="pl-3.5 pr-2 pt-2.5 flex items-center gap-2 min-w-0">
                <span className="text-[13.5px] font-semibold text-slate-900 min-w-0 [overflow-wrap:anywhere]">{d.domain}</span>
                <span className="shrink-0 h-[18px] px-1.5 rounded-full bg-slate-50 border border-slate-200 text-slate-500 text-[10.5px] font-medium inline-flex items-center">
                    {d.kind === "owned" ? "Already yours" : `${d.years} ${d.years === 1 ? "year" : "years"}`}
                </span>
                <AnimatePresence initial={false}>
                    {d.custom && (
                        <motion.span
                            key="custom"
                            initial={{ opacity: 0, scale: 0.9 }}
                            animate={{ opacity: 1, scale: 1 }}
                            exit={{ opacity: 0, scale: 0.9 }}
                            className="hidden sm:inline-flex shrink-0 h-[18px] px-1.5 rounded-full bg-sky-50 border border-sky-100 text-sky-700 text-[10.5px] font-medium items-center"
                        >
                            Customized
                        </motion.span>
                    )}
                </AnimatePresence>
                <div className="ml-auto flex items-center gap-1 shrink-0">
                    <Segmented
                        label={`Mail provider for ${d.domain}`}
                        pillId={`provider-${d.domain}`}
                        value={provider}
                        onChange={(p) => dispatch({ type: "provider", domain: d.domain, provider: p })}
                        title={d.providerFixed ? `New mailboxes match the ${PROVIDER_NAMES[provider]} ones already on ${d.domain}.` : undefined}
                        className="h-7"
                        options={options.map((p) => ({
                            value: p,
                            name: PROVIDER_NAMES[p],
                            label: (
                                <>
                                    <ProviderLogo id={p} size="xs" framed={false} />
                                    <span className="hidden sm:inline">{PROVIDER_SHORT[p]}</span>
                                </>
                            ),
                        }))}
                    />
                    <button
                        type="button"
                        onClick={() => dispatch({ type: "remove", domain: d.domain })}
                        aria-label={`Remove ${d.domain}`}
                        className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
            </header>

            <div className="px-3.5 pb-3 pt-2 space-y-2">
                <ul className="flex flex-wrap gap-1.5" aria-label={`Mailboxes on ${d.domain}`}>
                    <AnimatePresence initial={false} mode="popLayout">
                        {boxes.map((m, i) => (
                            <AddressChip key={`${m.sender.id}-${i}`} m={m} index={cart.senders.indexOf(m.sender)} domain={d.domain} admin={admin === i} bad={!!problems[i]} />
                        ))}
                    </AnimatePresence>
                </ul>
                <AnimatePresence initial={false}>
                    {note && (
                        <motion.p
                            key="note"
                            role="status"
                            initial={{ opacity: 0, height: 0 }}
                            animate={{ opacity: 1, height: "auto" }}
                            exit={{ opacity: 0, height: 0 }}
                            transition={{ duration: 0.18, ease: EASE }}
                            className="overflow-hidden text-[11px] text-amber-800 leading-snug"
                        >
                            {note}
                        </motion.p>
                    )}
                </AnimatePresence>

                <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                    <button
                        type="button"
                        onClick={() => setEditing(!open)}
                        aria-expanded={open}
                        aria-controls={editorId}
                        className={cn(
                            "h-6 -ml-1 px-1 rounded inline-flex items-center gap-1 text-[11.5px] font-medium outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                            issue ? "text-amber-800" : "text-slate-600 hover:text-slate-900",
                        )}
                    >
                        {issue ? <AlertCircleIcon className="w-3 h-3" /> : <SlidersHorizontalIcon className="w-3 h-3" />}
                        {issue ?? (open ? "Done" : "Customize")}
                        <ChevronDownIcon className={cn("w-3 h-3 text-slate-400 transition-transform", open && "rotate-180")} />
                    </button>
                    {d.kind === "new" && <ForwardLine cart={cart} d={d} dispatch={dispatch} shared={sharedForward} />}
                </div>

                <Collapse open={open} id={editorId}>
                    <Customize cart={cart} d={d} boxes={boxes} problems={problems} admin={admin} dispatch={dispatch} limits={limits} />
                </Collapse>
            </div>
        </article>
    );
}

const AddressChip = React.forwardRef<HTMLLIElement, { m: CartMailbox; index: number; domain: string; admin: boolean; bad: boolean }>(function AddressChip(
    { m, index, domain, admin, bad },
    ref,
) {
    const reduced = useReducedMotion();
    return (
        <motion.li
            ref={ref}
            layout={reduced ? false : "position"}
            initial={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.85 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.85, transition: { duration: 0.12 } }}
            transition={reduced ? { duration: 0.12 } : PRESS_SPRING}
            title={`${m.local_part}@${domain} · ${[m.sender.first, m.sender.last].filter(Boolean).join(" ")}${admin ? " · admin" : ""}`}
            className={cn(
                "h-6 pl-0.5 pr-2 rounded-full border inline-flex items-center gap-1.5 max-w-full",
                bad ? "border-red-200 bg-red-50 text-red-800" : "border-slate-200 bg-slate-50/70 text-slate-700",
            )}
        >
            <SenderAvatar photoId={m.sender.photo?.id} first={m.sender.first} last={m.sender.last} tone={index} className="size-5 text-[8.5px]" />
            <span className="min-w-0 truncate text-[11.5px] font-medium">
                {m.local_part || "…"}
                <span className="text-slate-400 font-normal">@</span>
            </span>
            {admin && <ShieldCheckIcon className="w-3 h-3 text-sky-600 shrink-0" aria-label="Admin" />}
        </motion.li>
    );
});

function Collapse({ open, id, children }: { open: boolean; id?: string; children: React.ReactNode }) {
    const reduced = useReducedMotion();
    return (
        <AnimatePresence initial={false}>
            {open && (
                <motion.div
                    id={id}
                    key="body"
                    initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                    transition={{ duration: 0.22, ease: EASE }}
                    className="overflow-hidden"
                >
                    {children}
                </motion.div>
            )}
        </AnimatePresence>
    );
}

/** This domain's own mailboxes: how many, whose each is, and its address. Untyped addresses keep following the senders. */
function Customize({
    cart,
    d,
    boxes,
    problems,
    admin,
    dispatch,
    limits,
}: {
    cart: Cart;
    d: CartDomain;
    boxes: CartMailbox[];
    problems: ReturnType<typeof domainProblems>;
    admin: number | null;
    dispatch: React.Dispatch<CartAction>;
    limits: CartLimits;
}) {
    const reduced = useReducedMotion();
    return (
        <div className="pt-2 mt-1 border-t border-slate-100 space-y-2.5">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <CountStepper
                    value={boxes.length}
                    min={1}
                    max={limits.max}
                    onChange={(n) => dispatch({ type: "slot_count", domain: d.domain, count: n, limits })}
                    label={`Mailboxes on ${d.domain}`}
                    unit="mailbox"
                />
                <span className="min-w-0 flex-1 text-[11px] text-slate-500 leading-snug">
                    {d.custom ? "Only this domain. Addresses you have not typed still follow the senders." : "Following the senders. A change here applies to this domain only."}
                </span>
                {d.custom && (
                    <button
                        type="button"
                        onClick={() => dispatch({ type: "uncustomize", domain: d.domain })}
                        className="h-6 px-1.5 rounded inline-flex items-center gap-1 text-[11.5px] font-medium text-sky-700 hover:text-sky-900 outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    >
                        <RotateCcwIcon className="w-3 h-3" /> Follow the senders
                    </button>
                )}
            </div>

            <ul className="space-y-1.5" role={admin !== null ? "radiogroup" : undefined} aria-label={admin !== null ? `Admin mailbox for ${d.domain}` : undefined}>
                <AnimatePresence initial={false}>
                    {boxes.map((m, i) => {
                        const p = problems[i];
                        const id = `buy-${d.domain}-${i}`;
                        return (
                            <motion.li
                                key={i}
                                initial={reduced ? false : { opacity: 0, height: 0 }}
                                animate={{ opacity: 1, height: "auto" }}
                                exit={{ opacity: 0, height: 0 }}
                                transition={{ duration: 0.18, ease: EASE }}
                                className="overflow-hidden"
                            >
                                <label htmlFor={id} className="sr-only">
                                    Address {i + 1} on {d.domain}
                                </label>
                                <div className="flex items-center gap-1.5 sm:gap-2">
                                    <SenderPick cart={cart} m={m} onPick={(sender) => dispatch({ type: "slot_sender", domain: d.domain, index: i, sender })} />
                                    <div
                                        className={cn(
                                            "flex-1 min-w-0 flex items-center h-8 rounded-md border bg-white transition-colors focus-within:ring-2",
                                            p ? "border-red-300 focus-within:border-red-400 focus-within:ring-red-100" : "border-slate-200 focus-within:border-sky-400 focus-within:ring-sky-100",
                                        )}
                                    >
                                        <input
                                            id={id}
                                            value={m.local_part}
                                            onChange={(e) => dispatch({ type: "local_part", domain: d.domain, index: i, value: e.target.value })}
                                            aria-invalid={!!p || undefined}
                                            aria-describedby={p ? `${id}-err` : undefined}
                                            autoComplete="off"
                                            spellCheck={false}
                                            autoCapitalize="none"
                                            className={cn("flex-1 min-w-0 h-full pl-2.5 bg-transparent outline-none text-[16px] md:text-[12.5px]", m.typed ? "text-slate-900" : "text-slate-600")}
                                        />
                                        <span className="hidden sm:inline pr-1.5 text-[12.5px] text-slate-400 truncate max-w-[45%] select-none" title={`@${d.domain}`}>@{d.domain}</span>
                                        {m.typed && (
                                            <button
                                                type="button"
                                                onClick={() => dispatch({ type: "auto_address", domain: d.domain, index: i })}
                                                aria-label={`Name address ${i + 1} after its sender again`}
                                                title="Follow the sender's name again"
                                                className="mr-1 size-6 rounded text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                                            >
                                                <RotateCcwIcon className="w-3 h-3" />
                                            </button>
                                        )}
                                    </div>
                                    {admin !== null && (
                                        <AdminRadio on={admin === i} label={`${m.local_part || `Address ${i + 1}`}@${d.domain}`} onPick={() => dispatch({ type: "admin", domain: d.domain, index: i })} />
                                    )}
                                </div>
                                <AnimatePresence initial={false}>
                                    {p && (
                                        <motion.p
                                            id={`${id}-err`}
                                            initial={{ opacity: 0, y: -2 }}
                                            animate={{ opacity: 1, y: 0 }}
                                            exit={{ opacity: 0 }}
                                            className="mt-1 flex items-center gap-1 text-[11px] text-red-600"
                                        >
                                            <AlertCircleIcon className="w-3 h-3" />
                                            {localPartMessage(p)}
                                        </motion.p>
                                    )}
                                </AnimatePresence>
                            </motion.li>
                        );
                    })}
                </AnimatePresence>
            </ul>
            {admin !== null && (
                <p className="flex items-start gap-1.5 text-[11px] text-slate-500 leading-relaxed">
                    <ShieldCheckIcon className="w-3 h-3 mt-0.5 text-slate-400 shrink-0" />
                    The admin mailbox also manages the {PROVIDER_NAMES[effectiveProvider(cart, d)]} account for {d.domain}. It sends and warms up like the others.
                </p>
            )}
        </div>
    );
}

/** Whose mailbox this is: the sender's avatar, opening a short menu of the senders. */
function SenderPick({ cart, m, onPick }: { cart: Cart; m: CartMailbox; onPick: (id: string) => void }) {
    const name = (s: { first: string; last: string }, i: number) => [s.first, s.last].filter(Boolean).join(" ") || `Sender ${i + 1}`;
    const at = cart.senders.indexOf(m.sender);
    if (cart.senders.length === 1) return <SenderAvatar photoId={m.sender.photo?.id} first={m.sender.first} last={m.sender.last} tone={0} className="size-7 text-[10px]" />;
    return (
        <PopoverMenu align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    aria-label={`Sender: ${name(m.sender, at)}. Change`}
                    className="h-8 pl-0.5 pr-1 rounded-full border border-slate-200 bg-white hover:border-slate-300 inline-flex items-center gap-0.5 shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors"
                >
                    <SenderAvatar photoId={m.sender.photo?.id} first={m.sender.first} last={m.sender.last} tone={at} className="size-6 text-[9px]" />
                    <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={200}>
                {cart.senders.map((s, i) => (
                    <PopoverMenuItem
                        key={s.id}
                        onSelect={() => onPick(s.id)}
                        selected={s.id === m.sender.id}
                        icon={<SenderAvatar photoId={s.photo?.id} first={s.first} last={s.last} tone={i} className="size-5 text-[8.5px]" />}
                        trailing={s.id === m.sender.id ? <CheckIcon className="w-3.5 h-3.5 text-sky-600" /> : undefined}
                    >
                        {name(s, i)}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

/** Marks one mailbox as the domain's admin; always visible, so it works on touch. */
function AdminRadio({ on, label, onPick }: { on: boolean; label: string; onPick: () => void }) {
    const reduced = useReducedMotion();
    return (
        <motion.button
            type="button"
            role="radio"
            aria-checked={on}
            aria-label={`Make ${label} the admin`}
            title={on ? "Admin of the domain's account" : "Make this the admin"}
            onClick={onPick}
            whileTap={reduced ? undefined : { scale: 0.94 }}
            transition={PRESS_SPRING}
            className={cn(
                "h-8 px-2 rounded-md border inline-flex items-center gap-1.5 text-[11px] font-medium shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                on ? "border-sky-300 bg-sky-50 text-sky-800" : "border-slate-200 bg-white text-slate-400 hover:text-slate-700 hover:border-slate-300",
            )}
        >
            <span className={cn("size-3 rounded-full border inline-flex items-center justify-center transition-colors", on ? "border-sky-600 bg-sky-600" : "border-slate-300")}>
                {on && <span className="size-1 rounded-full bg-white" />}
            </span>
            <span className="hidden sm:inline">Admin</span>
        </motion.button>
    );
}

/** The host a forward lands on, for the one-line summary. */
function forwardHost(url: string): string {
    try {
        const u = new URL(normalizeForwarding(url) || url);
        const host = u.host.replace(/^www\./, "");
        return u.pathname && u.pathname !== "/" ? host + u.pathname : host;
    } catch {
        return url;
    }
}

/** Where visitors to the new domains go: one value for the order, one quiet line on each new domain. */
function ForwardLine({ cart, d, dispatch, shared }: { cart: Cart; d: CartDomain; dispatch: React.Dispatch<CartAction>; shared: boolean }) {
    const [editing, setEditing] = React.useState(false);
    const problem = forwardingProblem(cart.forwarding);
    const value = cart.forwarding.trim();
    const open = editing || !!problem;
    const id = `buy-forward-${d.domain}`;
    if (!open) {
        return (
            <button
                type="button"
                onClick={() => setEditing(true)}
                className="min-w-0 max-w-full h-6 px-1 -mx-1 rounded inline-flex items-center gap-1 text-[11.5px] text-slate-500 hover:text-slate-900 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
            >
                <ArrowUpRightIcon className="w-3 h-3 shrink-0 text-slate-400" />
                {value ? (
                    <span className="min-w-0 truncate">
                        Visitors go to <span className="font-medium text-slate-700">{forwardHost(value)}</span>
                    </span>
                ) : (
                    <span className="truncate">Forward visitors to your website</span>
                )}
            </button>
        );
    }
    return (
        <div className="basis-full">
            <label htmlFor={id} className="flex items-center gap-1 mb-1 text-[11px] text-slate-500">
                <ArrowUpRightIcon className="w-3 h-3" /> Website forward <span className="text-slate-400">(optional)</span>
            </label>
            <div className="flex items-center gap-2">
                <TextInput
                    id={id}
                    autoFocus={editing}
                    value={cart.forwarding}
                    onChange={(v) => dispatch({ type: "forwarding", value: v })}
                    onKeyDown={(e: React.KeyboardEvent<HTMLInputElement>) => {
                        if ((e.key === "Enter" || e.key === "Escape") && !problem) setEditing(false);
                    }}
                    placeholder="https://yourcompany.com"
                    invalid={!!problem}
                    className="w-full"
                />
                <button
                    type="button"
                    onClick={() => {
                        dispatch({ type: "forwarding", value: "" });
                        setEditing(false);
                    }}
                    aria-label="Remove the website forward"
                    title="No forward"
                    className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200 shrink-0"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
                {!problem && (
                    <button
                        type="button"
                        onClick={() => setEditing(false)}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 transition-colors shrink-0"
                    >
                        Done
                    </button>
                )}
            </div>
            <FieldError message={problem} />
            <p className="mt-1 text-[11px] text-slate-400">
                Anyone who opens {d.domain} lands on your real site.{shared ? " Every new domain in this order forwards here." : ""}
            </p>
        </div>
    );
}
