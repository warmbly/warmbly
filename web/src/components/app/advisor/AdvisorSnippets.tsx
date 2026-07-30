// The exact values to paste, for a fix that lives outside Warmbly.
//
// "Add an SPF record" is where most people stop, because the hard part was never
// the instruction, it was knowing what to type. So the DNS findings hand over
// the record itself: the type, the host, the value, each on its own copy button,
// laid out the way a DNS provider's own form asks for them.
//
// Copying is one click and never a text-selection exercise. A value with a
// trailing space pasted into a TXT record fails silently, which is the worst
// class of bug to hand somebody.

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, CopyIcon } from "lucide-react";
import type { AdvisorSnippet } from "@/lib/api/models/app/advisor/Advisor";

export default function AdvisorSnippets({ snippets }: { snippets: AdvisorSnippet[] }) {
    if (snippets.length === 0) return null;

    return (
        <div>
            <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                Paste this
            </p>
            <div className="mt-1.5 space-y-1">
                {snippets.map((snippet, i) => (
                    <motion.div
                        key={`${snippet.label}-${i}`}
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: i * 0.04, duration: 0.18 }}
                    >
                        <SnippetRow snippet={snippet} />
                    </motion.div>
                ))}
            </div>
        </div>
    );
}

function SnippetRow({ snippet }: { snippet: AdvisorSnippet }) {
    const [copied, setCopied] = useState(false);

    async function copy() {
        if (!snippet.value) return;
        try {
            await navigator.clipboard.writeText(snippet.value);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1400);
        } catch {
            // Clipboard access can be refused (insecure origin, denied
            // permission). The value is selectable on screen either way, so
            // there is nothing useful to say about it.
        }
    }

    return (
        <div className="rounded-md border border-slate-200/70 bg-white/50 px-2.5 py-1.5">
            <div className="flex items-center gap-2">
                <span className="w-[104px] shrink-0 truncate text-[10.5px] uppercase tracking-[0.08em] text-slate-400">
                    {snippet.label}
                </span>

                {snippet.value ? (
                    <>
                        <code className="min-w-0 flex-1 truncate font-mono text-[11.5px] text-slate-800">
                            {snippet.value}
                        </code>
                        <button
                            type="button"
                            onClick={copy}
                            aria-label={`Copy ${snippet.label}`}
                            className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-slate-400 transition hover:bg-slate-500/10 hover:text-slate-700"
                        >
                            <AnimatePresence mode="wait" initial={false}>
                                {copied ? (
                                    <motion.span
                                        key="done"
                                        initial={{ scale: 0.6, opacity: 0 }}
                                        animate={{ scale: 1, opacity: 1 }}
                                        exit={{ scale: 0.6, opacity: 0 }}
                                        transition={{ duration: 0.14 }}
                                    >
                                        <CheckIcon className="h-3 w-3 text-emerald-600" />
                                    </motion.span>
                                ) : (
                                    <motion.span
                                        key="copy"
                                        initial={{ scale: 0.6, opacity: 0 }}
                                        animate={{ scale: 1, opacity: 1 }}
                                        exit={{ scale: 0.6, opacity: 0 }}
                                        transition={{ duration: 0.14 }}
                                    >
                                        <CopyIcon className="h-3 w-3" />
                                    </motion.span>
                                )}
                            </AnimatePresence>
                        </button>
                    </>
                ) : (
                    // No value to give: the note explains why, and a copy button
                    // for an empty string would be a trap.
                    <span className="min-w-0 flex-1 text-[11.5px] italic text-slate-400">
                        not available
                    </span>
                )}
            </div>

            {snippet.note ? (
                <p className="mt-1 pl-[112px] text-[11px] leading-relaxed text-slate-500">{snippet.note}</p>
            ) : null}
        </div>
    );
}
