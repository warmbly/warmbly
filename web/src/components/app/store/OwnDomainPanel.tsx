// The checkout's quiet second path: a domain registered elsewhere. Connect it (nameservers or DNS records), watch it
// go live, then add it to the order for mailboxes only. Domains already in the store can take mailboxes too.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ArrowLeftIcon, CheckIcon, ChevronRightIcon, GlobeIcon, Loader2Icon, PlusIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { FieldError, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import { useConnectStoreDomain, useStoreDomain, useStoreDomains } from "@/lib/api/hooks/app/store/useStore";
import type { ConnectMethod, StoreDomain } from "@/lib/api/models/app/store/Store";
import { looksLikeDomain, normalizeDomainInput, readyForMailboxes } from "@/lib/buy/connection";
import { connectErrorMessage, isPlanRequired, unavailableReason } from "@/lib/buy/errors";
import { HOVER_LIFT, PRESS_SPRING } from "@/lib/buy/motion";
import { cn } from "@/lib/utils";
import ConnectionGuide from "./ConnectionGuide";
import PlanUpsell from "./PlanUpsell";
import { PressButton } from "./Press";

const METHODS: { key: ConnectMethod; title: string; note: string; recommended?: boolean }[] = [
    {
        key: "nameservers",
        title: "Change nameservers",
        note: "Two values at your registrar. Warmbly keeps every record right from then on.",
        recommended: true,
    },
    {
        key: "manual",
        title: "Add DNS records",
        note: "Keep your DNS where it is and add a few records yourself.",
    },
];

// Answers that end a request without running it free its idempotency key.
const KEY_FREED = new Set([401, 403, 429]);

export default function OwnDomainPanel({
    inCart,
    onAdd,
    onBack,
}: {
    inCart: (domain: string) => boolean;
    onAdd: (d: StoreDomain) => void;
    onBack: () => void;
}) {
    const reduced = useReducedMotion();
    const list = useStoreDomains();
    const connect = useConnectStoreDomain();
    const [raw, setRaw] = React.useState("");
    const [method, setMethod] = React.useState<ConnectMethod>("nameservers");
    const [error, setError] = React.useState<string | null>(null);
    const [planNeeded, setPlanNeeded] = React.useState(false);
    const [openId, setOpenId] = React.useState<string | null>(null);
    const attempt = React.useRef<{ sig: string; key: string } | null>(null);

    const domain = normalizeDomainInput(raw);
    const shapeProblem = raw.trim() && !looksLikeDomain(domain) ? "Enter a domain name, like acme.com." : null;
    const yours = (list.data?.data ?? []).filter(
        (d) => d.status !== "removed" && d.status !== "transferred_out" && d.status !== "failed" && (d.source === "connected" || d.status === "active"),
    );

    const submit = async () => {
        if (!domain || shapeProblem || connect.isPending) return;
        setError(null);
        const sig = `${domain}:${method}`;
        if (attempt.current?.sig !== sig) attempt.current = { sig, key: crypto.randomUUID() };
        try {
            const d = await connect.mutateAsync({ domain, method, idempotencyKey: attempt.current.key });
            setRaw("");
            setOpenId(d.id);
        } catch (e) {
            const err = e as AppError;
            if (err.status && err.status < 500 && !KEY_FREED.has(err.status)) attempt.current = null;
            if (isPlanRequired(err)) {
                setPlanNeeded(true);
                return;
            }
            // Already here: open the one the workspace has instead of refusing.
            const existing = unavailableReason(err) === "already_added" ? yours.find((x) => x.name === domain) : undefined;
            if (existing) {
                setRaw("");
                setOpenId(existing.id);
                return;
            }
            setError(connectErrorMessage(err));
        }
    };

    if (planNeeded) return <PlanUpsell compact className="mt-4" />;

    return (
        <div className="mt-4">
            <AnimatePresence mode="wait" initial={false}>
                {openId ? (
                    <motion.div
                        key="guide"
                        initial={reduced ? { opacity: 0 } : { opacity: 0, x: 24 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, x: 24 }}
                        transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
                    >
                        <GuideView id={openId} seed={yours.find((d) => d.id === openId)} inCart={inCart} onAdd={onAdd} onBack={() => setOpenId(null)} />
                    </motion.div>
                ) : (
                    <motion.div
                        key="form"
                        initial={reduced ? { opacity: 0 } : { opacity: 0, x: -24 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, x: -24 }}
                        transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
                        className="space-y-3"
                    >
                        <section className="rounded-lg border border-slate-200 bg-white p-3.5 space-y-3">
                            <div>
                                <label htmlFor="own-domain" className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                    Your domain
                                </label>
                                <TextInput
                                    id="own-domain"
                                    value={raw}
                                    onChange={(v) => {
                                        setRaw(v);
                                        setError(null);
                                    }}
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter") void submit();
                                    }}
                                    placeholder="acme.com"
                                    autoComplete="off"
                                    invalid={!!shapeProblem || !!error}
                                    className="mt-1.5 w-full h-8"
                                />
                                <FieldError message={shapeProblem ?? error} />
                            </div>
                            <div role="radiogroup" aria-label="How to connect it" className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                                {METHODS.map((m) => {
                                    const on = method === m.key;
                                    return (
                                        <motion.button
                                            key={m.key}
                                            type="button"
                                            role="radio"
                                            aria-checked={on}
                                            onClick={() => setMethod(m.key)}
                                            whileHover={reduced ? undefined : HOVER_LIFT}
                                            whileTap={reduced ? undefined : { scale: 0.98 }}
                                            transition={PRESS_SPRING}
                                            className={cn(
                                                "text-left rounded-lg border px-3 py-2.5 flex items-start gap-2.5 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                                                on ? "border-sky-400 bg-sky-50/50" : "border-slate-200 bg-white hover:border-slate-300",
                                            )}
                                        >
                                            <span
                                                className={cn(
                                                    "mt-0.5 size-4 rounded-full border inline-flex items-center justify-center shrink-0 transition-colors",
                                                    on ? "border-sky-600 bg-sky-600" : "border-slate-300 bg-white",
                                                )}
                                            >
                                                {on && <span className="size-1.5 rounded-full bg-white" />}
                                            </span>
                                            <span className="min-w-0">
                                                <span className="flex items-center gap-1.5 text-[12.5px] font-medium text-slate-900">
                                                    {m.title}
                                                    {m.recommended && (
                                                        <span className="h-[18px] px-1.5 rounded-full bg-emerald-50 border border-emerald-100 text-emerald-700 text-[10px] font-medium inline-flex items-center">
                                                            Recommended
                                                        </span>
                                                    )}
                                                </span>
                                                <span className="block mt-0.5 text-[11px] text-slate-500 leading-snug">{m.note}</span>
                                            </span>
                                        </motion.button>
                                    );
                                })}
                            </div>
                            <div className="flex flex-wrap items-center gap-2">
                                <PressButton onClick={() => void submit()} disabled={!domain || !!shapeProblem || connect.isPending} className="h-8 px-3">
                                    {connect.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <ChevronRightIcon className="w-3.5 h-3.5" />}
                                    {connect.isPending ? "Connecting…" : "Connect domain"}
                                </PressButton>
                                <span className="text-[11px] text-slate-400">Nothing is charged for a domain you already own.</span>
                            </div>
                        </section>

                        {yours.length > 0 && (
                            <section aria-label="Domains you already have">
                                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">Domains you already have</div>
                                <ul className="rounded-lg border border-slate-200 divide-y divide-slate-100 bg-white overflow-hidden">
                                    {yours.map((d) => (
                                        <YourDomainRow key={d.id} d={d} added={inCart(d.name)} onOpen={() => setOpenId(d.id)} onAdd={() => onAdd(d)} />
                                    ))}
                                </ul>
                            </section>
                        )}

                        <button type="button" onClick={onBack} className="inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-slate-900 transition-colors">
                            <ArrowLeftIcon className="w-3 h-3" /> Search for a new domain instead
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function rowState(d: StoreDomain): { text: string; tone: string } {
    if (d.source !== "connected") return { text: d.mailbox_count > 0 ? `Bought here · ${d.mailbox_count} ${d.mailbox_count === 1 ? "mailbox" : "mailboxes"}` : "Bought here", tone: "text-slate-500" };
    const c = d.connection?.status;
    if (c === "active") return (d.dns_conflicts ?? []).length > 0 ? { text: "Records in the way of mail", tone: "text-amber-700" } : { text: "Connected", tone: "text-emerald-700" };
    if (c === "expired") return { text: "Instructions expired", tone: "text-amber-700" };
    if (c === "moved") return { text: "DNS moved away", tone: "text-red-600" };
    return { text: "Waiting for DNS", tone: "text-sky-700" };
}

function YourDomainRow({ d, added, onOpen, onAdd }: { d: StoreDomain; added: boolean; onOpen: () => void; onAdd: () => void }) {
    const ready = readyForMailboxes(d);
    const s = rowState(d);
    return (
        <li>
            <div
                role="button"
                tabIndex={0}
                onClick={onOpen}
                onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onOpen();
                    }
                }}
                className="px-3 sm:px-4 py-2.5 flex items-center gap-3 hover:bg-slate-50/70 cursor-pointer transition-colors outline-none focus-visible:bg-slate-50"
            >
                {d.provider ? (
                    <ProviderLogo id={d.provider} size="md" />
                ) : (
                    <span className="size-6 rounded-md border border-slate-200 inline-flex items-center justify-center shrink-0">
                        <GlobeIcon className="w-3 h-3 text-slate-400" />
                    </span>
                )}
                <div className="min-w-0 flex-1">
                    <div className="text-[13px] font-medium text-slate-900 truncate">{d.name}</div>
                    <div className={cn("text-[11px] truncate", s.tone)}>{s.text}</div>
                </div>
                {ready ? (
                    <button
                        type="button"
                        onClick={(e) => {
                            e.stopPropagation();
                            onAdd();
                        }}
                        aria-pressed={added}
                        aria-label={added ? `Remove ${d.name} from the order` : `Add mailboxes on ${d.name}`}
                        className={cn(
                            "h-7 rounded-full inline-flex items-center justify-center gap-1 text-[12px] font-medium shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors",
                            added ? "w-7 bg-sky-600 text-white hover:bg-sky-700" : "px-2.5 border border-slate-200 bg-white text-slate-800 hover:border-sky-300 hover:text-sky-800",
                        )}
                    >
                        {added ? (
                            <CheckIcon className="w-3.5 h-3.5" strokeWidth={2.75} />
                        ) : (
                            <>
                                <PlusIcon className="w-3 h-3" /> Add
                            </>
                        )}
                    </button>
                ) : (
                    <ChevronRightIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                )}
            </div>
        </li>
    );
}

function GuideView({
    id,
    seed,
    inCart,
    onAdd,
    onBack,
}: {
    id: string;
    seed: StoreDomain | undefined;
    inCart: (domain: string) => boolean;
    onAdd: (d: StoreDomain) => void;
    onBack: () => void;
}) {
    const detail = useStoreDomain(id);
    const d = detail.data ?? seed;
    if (!d) {
        return (
            <div className="py-10 flex justify-center text-slate-300" aria-busy="true">
                <Loader2Icon className="w-4 h-4 animate-spin" />
            </div>
        );
    }
    const ready = readyForMailboxes(d);
    const added = inCart(d.name);
    return (
        <div className="space-y-3">
            <button type="button" onClick={onBack} className="inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-slate-900 transition-colors">
                <ArrowLeftIcon className="w-3 h-3" /> Use another domain
            </button>
            <section className="rounded-lg border border-slate-200 bg-white overflow-hidden">
                <header className="h-11 px-3.5 flex items-center gap-2 border-b border-slate-100 bg-slate-50/50">
                    <GlobeIcon className="w-3.5 h-3.5 text-slate-400" />
                    <span className="text-[13px] font-semibold text-slate-900 truncate">{d.name}</span>
                </header>
                <div className="p-3.5">
                    {d.connection ? (
                        <ConnectionGuide domain={d} canEdit />
                    ) : (
                        <p className="text-[12px] text-slate-600">{ready ? `${d.name} is ready for mailboxes.` : `${d.name} is still being set up.`}</p>
                    )}
                </div>
                <AnimatePresence initial={false}>
                    {ready && (
                        <motion.div
                            initial={{ opacity: 0, height: 0 }}
                            animate={{ opacity: 1, height: "auto" }}
                            exit={{ opacity: 0, height: 0 }}
                            className="overflow-hidden border-t border-slate-100 bg-sky-50/40"
                        >
                            <div className="px-3.5 py-2.5 flex flex-wrap items-center gap-2">
                                <span className="min-w-0 flex-1 text-[12px] text-slate-700">
                                    {added ? `${d.name} is in your order. Set its mailboxes there.` : `Add ${d.name} to buy mailboxes on it. Only the mailboxes are charged.`}
                                </span>
                                {!added && (
                                    <PressButton onClick={() => onAdd(d)}>
                                        <PlusIcon className="w-3 h-3" /> Add to order
                                    </PressButton>
                                )}
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>
            </section>
        </div>
    );
}
