// The buy page while the instance has no GraphKit API key: what the store does, and how it gets switched on.
import toast from "react-hot-toast/headless";
import { motion, useReducedMotion } from "framer-motion";
import { ArrowUpRightIcon, CheckIcon, CopyIcon } from "lucide-react";
import type { StoreStatus } from "@/lib/api/models/app/store/Store";
import { EASE, HOVER_LIFT, PRESS_SPRING } from "@/lib/buy/motion";
import { LockedStoreIllustration } from "./Illustrations";
import { PressButton } from "./Press";
import StorePaused from "./StorePaused";

export const STORE_SETUP_DOCS = "https://docs.warmbly.com/guides/buy-mailboxes/#self-hosted-instances";

/** The note a member sends their operator; plain text so it pastes anywhere. */
export const OPERATOR_MESSAGE = `Hi, could you add a GraphKit API key to our Warmbly instance? It lets us buy domains at cost and ready-to-send Google or Microsoft mailboxes that connect and start warming on their own. It goes in the admin panel under Setup and health > Integrations: ${STORE_SETUP_DOCS}`;

const BENEFITS = [
    "Domains at cost, registered next to your main one",
    "Ready-to-send Google or Microsoft mailboxes",
    "Connected and warming automatically",
];

const STEPS = [
    "Open Setup and health, then Integrations, in the admin panel.",
    "Paste your GraphKit API key and select Test connection.",
    "Save. The webhook secret is optional and can be added later.",
];

export default function StoreLocked({ status }: { status: StoreStatus }) {
    const reduced = useReducedMotion();
    const admin = status.can_configure === true;
    return (
        <motion.section
            initial={reduced ? false : { opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.28, ease: EASE }}
            aria-labelledby="store-locked-title"
            className="max-w-[560px] mx-auto py-6 sm:py-10"
        >
            {status.disconnected && <StorePaused status={status} className="mb-5" />}
            <div className="rounded-2xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_16px_40px_-24px_rgba(15,23,42,0.28)] px-5 py-7 sm:px-8 sm:py-8">
                <div className="flex flex-col items-center text-center">
                    <LockedStoreIllustration className="w-[200px] h-auto" />
                    <h1 id="store-locked-title" className="mt-3 text-[18px] font-semibold tracking-[-0.02em] text-slate-900">
                        Buy domains and mailboxes
                    </h1>
                    <p className="mt-1 text-[12.5px] text-slate-500 leading-relaxed max-w-[46ch]">This instance isn't set up for it yet. To buy domains and mailboxes, add a GraphKit API key.</p>
                </div>

                <ul className="mt-5 space-y-2" aria-label="What the store does">
                    {BENEFITS.map((b) => (
                        <li key={b} className="flex items-start gap-2 text-[12.5px] text-slate-700">
                            <span className="mt-0.5 size-4 rounded-full bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                                <CheckIcon className="w-2.5 h-2.5" strokeWidth={3} />
                            </span>
                            {b}
                        </li>
                    ))}
                </ul>

                <div className="mt-6 pt-5 border-t border-slate-100">{admin ? <AdminSetup url={status.configure_url} /> : <AskOperator />}</div>
            </div>
        </motion.section>
    );
}

function AdminSetup({ url }: { url: string | undefined }) {
    const reduced = useReducedMotion();
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Set it up</div>
            {!url && <p className="mt-1.5 text-[12px] text-slate-500 leading-relaxed">Open the admin panel's Setup and health page to add the key.</p>}
            <ol className="mt-3 space-y-2.5" aria-label="Setup steps">
                {STEPS.map((s, i) => (
                    <li key={s} className="flex items-start gap-2.5 text-[12.5px] text-slate-700 leading-relaxed">
                        <span className="size-5 rounded-full border border-slate-200 bg-slate-50 text-[10.5px] font-semibold text-slate-600 inline-flex items-center justify-center shrink-0 tabular-nums">
                            {i + 1}
                        </span>
                        <span className="min-w-0 pt-px">{s}</span>
                    </li>
                ))}
            </ol>
            {url && (
                <motion.span
                    whileHover={reduced ? undefined : HOVER_LIFT}
                    whileTap={reduced ? undefined : { scale: 0.97 }}
                    transition={PRESS_SPRING}
                    className="mt-5 inline-flex rounded-md"
                >
                    <a
                        href={url}
                        target="_blank"
                        rel="noreferrer"
                        className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-semibold inline-flex items-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-300 focus-visible:ring-offset-2"
                    >
                        Add GraphKit API key <ArrowUpRightIcon className="w-3.5 h-3.5" />
                    </a>
                </motion.span>
            )}
        </div>
    );
}

function AskOperator() {
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(OPERATOR_MESSAGE);
            toast.success("Message copied");
        } catch {
            toast.error("Copy did not work here. Select the text and copy it.");
        }
    };
    return (
        <div>
            <p className="text-[12.5px] font-medium text-slate-900">Ask your instance operator to add a GraphKit API key</p>
            <p className="mt-1 text-[12px] text-slate-500 leading-relaxed">It takes them a minute in the admin panel. Here is a note you can send.</p>
            <blockquote className="mt-3 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5 text-[12px] text-slate-600 leading-relaxed select-all break-words">
                {OPERATOR_MESSAGE}
            </blockquote>
            <PressButton tone="secondary" className="mt-3" onClick={() => void copy()}>
                <CopyIcon className="w-3 h-3" /> Copy message
            </PressButton>
        </div>
    );
}
