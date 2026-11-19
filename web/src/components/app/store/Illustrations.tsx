// Inline illustrations for the buy flow: the empty search, the finished order and the store still to be set up. Slate and sky only.
import { motion, useReducedMotion } from "framer-motion";

/** An address bar finding a domain, with a couple of results under it. */
export function SearchIllustration({ className }: { className?: string }) {
    return (
        <svg viewBox="0 0 240 150" fill="none" role="img" aria-hidden="true" className={className}>
            <ellipse cx="120" cy="80" rx="100" ry="62" className="fill-sky-50" />
            <circle cx="40" cy="38" r="2" className="fill-sky-300" />
            <circle cx="206" cy="52" r="2.5" className="fill-sky-200" />
            <path d="M200 112h8M204 108v8" strokeWidth="1.5" strokeLinecap="round" className="stroke-sky-300" />
            <path d="M28 96h6M31 93v6" strokeWidth="1.5" strokeLinecap="round" className="stroke-slate-300" />

            <g filter="url(#buy-soft)">
                <rect x="44" y="34" width="152" height="86" rx="10" className="fill-white stroke-slate-200" />
            </g>
            <circle cx="56" cy="44" r="2" className="fill-slate-200" />
            <circle cx="63" cy="44" r="2" className="fill-slate-200" />
            <circle cx="70" cy="44" r="2" className="fill-slate-200" />

            <rect x="56" y="54" width="128" height="18" rx="9" className="fill-slate-50 stroke-slate-200" />
            <circle cx="66" cy="63" r="4.5" strokeWidth="1.3" className="stroke-sky-500" />
            <path d="M61.5 63h9M66 58.5c-2 2.6-2 6.4 0 9M66 58.5c2 2.6 2 6.4 0 9" strokeWidth="1" className="stroke-sky-500" />
            <rect x="76" y="60" width="44" height="6" rx="3" className="fill-slate-300" />
            <rect x="122" y="60" width="18" height="6" rx="3" className="fill-sky-400" />
            <rect x="141" y="58" width="1.5" height="10" rx="0.75" className="fill-sky-500" />

            <rect x="56" y="82" width="74" height="5" rx="2.5" className="fill-slate-200" />
            <rect x="56" y="92" width="44" height="4" rx="2" className="fill-slate-100" />
            <circle cx="176" cy="88" r="7" className="fill-sky-100" />
            <path d="M172.8 88.2l2.2 2.2 4.2-4.6" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" className="stroke-sky-600" />

            <rect x="56" y="104" width="60" height="5" rx="2.5" className="fill-slate-100" />
            <rect x="162" y="102" width="22" height="9" rx="4.5" className="fill-white stroke-slate-200" />

            <g transform="translate(150 18)">
                <rect width="58" height="18" rx="9" className="fill-white stroke-slate-200" />
                <circle cx="10" cy="9" r="3" className="fill-emerald-400" />
                <rect x="17" y="6.5" width="32" height="5" rx="2.5" className="fill-slate-200" />
            </g>
            <g transform="translate(18 112)">
                <rect width="52" height="18" rx="9" className="fill-white stroke-slate-200" />
                <circle cx="10" cy="9" r="3" className="fill-sky-400" />
                <rect x="17" y="6.5" width="26" height="5" rx="2.5" className="fill-slate-200" />
            </g>

            <defs>
                <filter id="buy-soft" x="24" y="22" width="192" height="126" filterUnits="userSpaceOnUse">
                    <feDropShadow dx="0" dy="6" stdDeviation="7" floodColor="#0f172a" floodOpacity="0.07" />
                </filter>
            </defs>
        </svg>
    );
}

const BURST = Array.from({ length: 10 }, (_, i) => {
    const a = (i / 10) * Math.PI * 2 + 0.3;
    return { x: Math.cos(a), y: Math.sin(a), big: i % 3 === 0 };
});

/** The finished order: a soft burst behind an envelope and a check that draws itself. */
export function DoneIllustration({ className }: { className?: string }) {
    const reduced = useReducedMotion();
    const still = !!reduced;
    return (
        <div className={className} style={{ position: "relative" }} aria-hidden="true">
            <svg viewBox="0 0 240 170" fill="none" className="w-full h-full overflow-visible">
                <motion.circle
                    cx="120"
                    cy="84"
                    r="70"
                    className="fill-sky-50"
                    initial={still ? false : { scale: 0.6, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ duration: 0.6, ease: [0.22, 1, 0.36, 1] }}
                    style={{ transformOrigin: "120px 84px" }}
                />
                <motion.circle
                    cx="120"
                    cy="84"
                    r="54"
                    strokeWidth="1"
                    className="stroke-sky-100"
                    initial={still ? false : { scale: 0.4, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ duration: 0.7, delay: 0.05, ease: [0.22, 1, 0.36, 1] }}
                    style={{ transformOrigin: "120px 84px" }}
                />
                {!still &&
                    BURST.map((p, i) => (
                        <motion.circle
                            key={i}
                            cx={120}
                            cy={84}
                            r={p.big ? 3 : 2}
                            className={p.big ? "fill-sky-400" : "fill-sky-200"}
                            initial={{ x: 0, y: 0, opacity: 0, scale: 0.4 }}
                            animate={{ x: p.x * 86, y: p.y * 70, opacity: [0, 1, 0], scale: [0.4, 1, 0.6] }}
                            transition={{ duration: 1.1, delay: 0.25 + i * 0.015, ease: "easeOut" }}
                        />
                    ))}

                <motion.g
                    initial={still ? false : { y: 10, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    transition={{ duration: 0.5, delay: 0.1, ease: [0.22, 1, 0.36, 1] }}
                >
                    <rect x="78" y="60" width="84" height="58" rx="8" className="fill-white stroke-slate-200" />
                    <path d="M80 64l40 28 40-28" strokeWidth="1.5" strokeLinejoin="round" className="stroke-slate-200" />
                    <rect x="92" y="98" width="30" height="4" rx="2" className="fill-slate-100" />
                    <rect x="92" y="106" width="18" height="4" rx="2" className="fill-slate-100" />
                </motion.g>

                <motion.g
                    initial={still ? false : { scale: 0, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ type: "spring", stiffness: 360, damping: 18, delay: 0.3 }}
                    style={{ transformOrigin: "160px 60px" }}
                >
                    <circle cx="160" cy="60" r="17" className="fill-sky-600" />
                    <circle cx="160" cy="60" r="17" strokeWidth="3" className="stroke-white" />
                    <motion.path
                        d="M152.5 60.5l5 5 10-11"
                        strokeWidth="3"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        className="stroke-white"
                        initial={still ? false : { pathLength: 0 }}
                        animate={{ pathLength: 1 }}
                        transition={{ duration: 0.4, delay: 0.55, ease: "easeOut" }}
                    />
                </motion.g>
            </svg>
        </div>
    );
}

/** The store still to be set up: a storefront with its shutter half down and a padlock waiting on a key. */
export function LockedStoreIllustration({ className }: { className?: string }) {
    return (
        <svg viewBox="0 0 240 160" fill="none" role="img" aria-hidden="true" className={className}>
            <ellipse cx="120" cy="86" rx="102" ry="62" className="fill-sky-50" />
            <circle cx="36" cy="44" r="2" className="fill-sky-300" />
            <circle cx="210" cy="40" r="2.5" className="fill-sky-200" />
            <path d="M206 118h8M210 114v8" strokeWidth="1.5" strokeLinecap="round" className="stroke-sky-300" />
            <path d="M24 104h6M27 101v6" strokeWidth="1.5" strokeLinecap="round" className="stroke-slate-300" />

            <g filter="url(#locked-soft)">
                <rect x="58" y="44" width="124" height="88" rx="10" className="fill-white stroke-slate-200" />
            </g>
            <path d="M58 54a10 10 0 0 1 10-10h104a10 10 0 0 1 10 10v8H58z" className="fill-sky-100" />
            <path d="M74 44v18M90 44v18M106 44v18M122 44v18M138 44v18M154 44v18M170 44v18" strokeWidth="1" className="stroke-sky-200" />
            <path d="M58 62h124" className="stroke-slate-200" />

            <rect x="72" y="72" width="96" height="60" rx="3" className="fill-slate-50 stroke-slate-200" />
            <path d="M72 80h96M72 88h96M72 96h96M72 104h96" strokeWidth="1" className="stroke-slate-200" />
            <rect x="80" y="110" width="30" height="16" rx="3" className="fill-white stroke-slate-200" />
            <path d="M85 115h20M85 120h12" strokeWidth="1.5" strokeLinecap="round" className="stroke-slate-200" />
            <rect x="130" y="110" width="30" height="16" rx="3" className="fill-white stroke-slate-200" />
            <path d="M135 115l10 6 10-6" strokeWidth="1.3" strokeLinejoin="round" className="stroke-sky-300" />

            <g transform="translate(150 86)">
                <circle cx="20" cy="22" r="22" className="fill-white stroke-slate-200" />
                <path d="M13 19v-4a7 7 0 0 1 14 0v4" strokeWidth="2.2" strokeLinecap="round" className="stroke-slate-400" />
                <rect x="10" y="19" width="20" height="15" rx="3.5" className="fill-sky-600" />
                <circle cx="20" cy="25.5" r="2" className="fill-white" />
                <path d="M20 27v3" strokeWidth="1.6" strokeLinecap="round" className="stroke-white" />
            </g>

            <g transform="translate(26 70)">
                <rect width="56" height="18" rx="9" className="fill-white stroke-slate-200" />
                <circle cx="10" cy="9" r="3" className="fill-sky-400" />
                <rect x="17" y="6.5" width="30" height="5" rx="2.5" className="fill-slate-200" />
            </g>

            <defs>
                <filter id="locked-soft" x="38" y="32" width="164" height="124" filterUnits="userSpaceOnUse">
                    <feDropShadow dx="0" dy="6" stdDeviation="7" floodColor="#0f172a" floodOpacity="0.07" />
                </filter>
            </defs>
        </svg>
    );
}
