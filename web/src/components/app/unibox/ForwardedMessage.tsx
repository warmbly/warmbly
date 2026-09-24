// The message a forward carries, shown under the note and signature in the
// order they are sent. Read-only: the server attaches the stored message, so
// this is a preview of what goes out, not something the sender edits.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { AlertCircleIcon, ChevronDownIcon, ForwardIcon } from "lucide-react";
import EmailBody from "./EmailBody";
import useUniboxEmail from "@/lib/api/hooks/app/unibox/useUniboxEmail";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import { formatExactTime, recipientsOf, summarizeAddresses } from "@/lib/unibox/messageDetails";
import { cn } from "@/lib/utils";

export default function ForwardedMessage({ email }: { email: UniboxEmail }) {
    const [open, setOpen] = React.useState(false);
    const detail = useUniboxEmail(email.id);
    const data = detail.data;

    const from = data?.from?.length ? data.from : email.from ? [email.from] : [];
    const to = recipientsOf(email, data);
    const cc = data?.cc ?? [];
    const subject = data?.subject || email.subject || "(no subject)";
    const date = new Date(data?.date ?? email.date);

    return (
        <div className="mx-4 mb-2 rounded-md border border-slate-200 overflow-hidden">
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                aria-expanded={open}
                title={open ? "Hide the forwarded message" : "Show the forwarded message"}
                className="w-full px-3 py-1.5 flex items-center gap-1.5 bg-slate-50 hover:bg-slate-100 text-left transition-colors"
            >
                <ForwardIcon className="w-3 h-3 shrink-0 text-violet-500" />
                <span className="shrink-0 text-[10px] uppercase tracking-[0.14em] text-slate-600 font-semibold">
                    Forwarded message
                </span>
                <span className="min-w-0 flex-1 truncate text-[11px] text-slate-400">
                    {summarizeAddresses(from, 1)} · {subject}
                </span>
                <ChevronDownIcon
                    className={cn("w-3 h-3 shrink-0 text-slate-400 transition-transform", open && "rotate-180")}
                />
            </button>

            {data?.body_truncated && (
                <p className="px-3 py-1.5 flex items-start gap-1.5 border-t border-amber-200/60 bg-amber-50/50 text-[11.5px] text-amber-900">
                    <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0 text-amber-700" />
                    Only a preview of this message is stored, so the forward carries that preview, not the full message.
                </p>
            )}

            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        key="forwarded"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
                        className="overflow-hidden border-t border-slate-200"
                    >
                        <dl className="px-3 pt-2 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-[11.5px]">
                            <Field label="From" value={from.join(", ")} />
                            <Field label="Date" value={Number.isFinite(date.getTime()) ? formatExactTime(date) : ""} />
                            <Field label="Subject" value={subject} />
                            <Field label="To" value={to.join(", ")} />
                            {cc.length > 0 && <Field label="Cc" value={cc.join(", ")} />}
                        </dl>
                        <div className="px-3 py-2 max-h-64 overflow-y-auto">
                            {detail.isPending ? (
                                <div className="space-y-2 py-0.5" aria-busy aria-label="Loading message">
                                    <div className="h-2.5 w-[85%] rounded bg-slate-100 animate-pulse" />
                                    <div className="h-2.5 w-[60%] rounded bg-slate-100 animate-pulse" />
                                </div>
                            ) : detail.isError ? (
                                <p className="text-[11.5px] text-slate-500">
                                    Couldn't load the preview. The message is still attached when you send.
                                </p>
                            ) : (
                                <EmailBody html={data?.body_html} plain={data?.body_plain} />
                            )}
                        </div>
                        <p className="px-3 pb-2 text-[11px] text-slate-400">
                            Attachments on the original are not forwarded.
                        </p>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function Field({ label, value }: { label: string; value: string }) {
    if (!value) return null;
    return (
        <>
            <dt className="text-slate-400">{label}</dt>
            <dd className="min-w-0 truncate text-slate-700" title={value}>
                {value}
            </dd>
        </>
    );
}
