// "Talk to sales" — Enterprise has no self-serve price, so its CTA opens this
// instead of a Stripe portal session (a prospect on the free tier has no
// Stripe customer to manage anyway). Posts to /subscription/enterprise-inquiry.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { Loader2Icon, SendIcon, XIcon } from "lucide-react";
import { useUserProfile } from "@/hooks/context/user";
import { useAppStore } from "@/stores";
import useEnterpriseInquiry from "@/lib/api/hooks/app/subscription/useEnterpriseInquiry";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { Label, NumberInput, TextInput } from "@/components/ui/field";

export default function EnterpriseInquiryDialog({
    open,
    onClose,
}: {
    open: boolean;
    onClose: () => void;
}) {
    const { user } = useUserProfile();
    const orgName = useAppStore((s) => s.currentOrganization?.name);
    const inquiry = useEnterpriseInquiry();

    const [company, setCompany] = React.useState("");
    const [name, setName] = React.useState("");
    const [email, setEmail] = React.useState("");
    const [volume, setVolume] = React.useState<number>(Number.NaN);
    const [teamSize, setTeamSize] = React.useState<number>(Number.NaN);
    const [notes, setNotes] = React.useState("");

    // Prefill from the signed-in user each time it opens, without clobbering
    // edits while it is on screen.
    React.useEffect(() => {
        if (!open) return;
        setCompany(orgName ?? "");
        setName([user.first_name, user.last_name].filter(Boolean).join(" "));
        setEmail(user.email ?? "");
    }, [open, orgName, user.first_name, user.last_name, user.email]);

    // This opens on top of the upgrade chooser, so it owns Escape and Tab
    // while it is up: move focus in, keep Tab inside, hand it back on close.
    // The chooser's own trap defers to any element marked data-nested-modal.
    const cardRef = React.useRef<HTMLDivElement>(null);
    React.useEffect(() => {
        if (!open) return;
        const previous = document.activeElement as HTMLElement | null;
        cardRef.current?.focus();

        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                if (!inquiry.isPending) {
                    e.stopPropagation();
                    onClose();
                }
                return;
            }
            if (e.key !== "Tab") return;
            const card = cardRef.current;
            if (!card) return;
            const focusable = card.querySelectorAll<HTMLElement>(
                'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
            );
            if (focusable.length === 0) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            const active = document.activeElement;
            e.stopPropagation();
            if (e.shiftKey && (active === first || active === card)) {
                e.preventDefault();
                last.focus();
            } else if (!e.shiftKey && active === last) {
                e.preventDefault();
                first.focus();
            }
        };
        // Capture phase so this runs before the chooser's document listener.
        document.addEventListener("keydown", onKey, true);
        return () => {
            document.removeEventListener("keydown", onKey, true);
            previous?.focus?.();
        };
    }, [open, inquiry.isPending, onClose]);

    const valid = company.trim() && name.trim() && /.+@.+\..+/.test(email.trim());

    async function submit() {
        if (!valid || inquiry.isPending) return;
        try {
            const res = await inquiry.mutateAsync({
                company_name: company.trim(),
                contact_name: name.trim(),
                contact_email: email.trim(),
                estimated_volume: Number.isFinite(volume) ? volume : undefined,
                team_size: Number.isFinite(teamSize) ? teamSize : undefined,
                notes: notes.trim() || undefined,
            });
            toast.success(res.message || "Thanks, our team will be in touch.");
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.16 }}
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget && !inquiry.isPending) onClose();
                    }}
                    className="fixed inset-0 z-[160] flex items-center justify-center bg-slate-900/40 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="enterprise-inquiry-title"
                        data-floating
                        data-nested-modal
                        ref={cardRef}
                        tabIndex={-1}
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 8 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[460px] max-h-[calc(100dvh-3rem)] overflow-y-auto rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)]"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 sticky top-0 bg-white">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Enterprise
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span id="enterprise-inquiry-title" className="text-[12.5px] text-slate-900 font-medium">
                                Talk to sales
                            </span>
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={inquiry.isPending}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="px-4 py-4 space-y-3">
                            <p className="text-[12.5px] text-slate-500 leading-relaxed">
                                Enterprise is priced on your volume and sending shape. Tell us where you are
                                and we will come back within one business day.
                            </p>
                            <div>
                                <Label>Company</Label>
                                <TextInput value={company} onChange={setCompany} placeholder="Acme Inc" />
                            </div>
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                <div>
                                    <Label>Your name</Label>
                                    <TextInput value={name} onChange={setName} placeholder="Jane Doe" />
                                </div>
                                <div>
                                    <Label>Work email</Label>
                                    <TextInput
                                        value={email}
                                        onChange={setEmail}
                                        type="email"
                                        placeholder="jane@acme.com"
                                    />
                                </div>
                            </div>
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                <div>
                                    <Label>Emails per day</Label>
                                    <NumberInput value={volume} onChange={setVolume} min={0} placeholder="25000" />
                                </div>
                                <div>
                                    <Label>Team size</Label>
                                    <NumberInput value={teamSize} onChange={setTeamSize} min={0} placeholder="25" />
                                </div>
                            </div>
                            <div>
                                <Label>Anything else</Label>
                                <textarea
                                    value={notes}
                                    onChange={(e) => setNotes(e.target.value)}
                                    rows={3}
                                    placeholder="Mailbox count, providers, compliance needs…"
                                    className="w-full rounded-md border border-slate-200 px-2.5 py-1.5 text-[12.5px] text-slate-900 placeholder:text-slate-400 focus:outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y"
                                />
                            </div>
                        </div>

                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5 sticky bottom-0 bg-white">
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={inquiry.isPending}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={!valid || inquiry.isPending}
                                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {inquiry.isPending ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <SendIcon className="w-3 h-3" />
                                )}
                                Send
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}
