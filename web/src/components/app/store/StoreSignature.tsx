// The order's signature: Default (the sender's name), None, or a template written in the mailbox signature editor with
// the same sender placeholders a bulk signature uses, filled in per mailbox at checkout.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { BracesIcon, ChevronDownIcon } from "lucide-react";
import EmailEditor from "@/components/app/EmailEditor";
import UnknownPlaceholders from "@/components/app/emails/UnknownPlaceholders";
import EmailBody from "@/components/app/unibox/EmailBody";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { mailboxSignature, previewMailbox, type Cart, type CartAction, type SignatureChoice } from "@/lib/buy/cart";
import { EASE } from "@/lib/buy/motion";
import { signatureToPlain } from "@/lib/email/signaturePlain";
import { SIGNATURE_PLACEHOLDERS, unknownPlaceholders } from "@/lib/email/signaturePlaceholders";
import { Segmented } from "./Controls";

type Caret = { el: HTMLTextAreaElement; start: number; end: number } | { el: HTMLElement; range: Range | null };

export default function StoreSignature({ cart, dispatch }: { cart: Cart; dispatch: React.Dispatch<CartAction> }) {
    const reduced = useReducedMotion();
    const { choice, custom } = cart.signature;
    const [code, setCode] = React.useState(false);
    const wrap = React.useRef<HTMLDivElement>(null);
    const caret = React.useRef<Caret | null>(null);
    const preview = previewMailbox(cart);
    const filled = mailboxSignature(cart.signature, preview.sender, preview.address);
    const unknown = choice === "custom" ? unknownPlaceholders(custom.plainSync ? custom.html : `${custom.html}\n${custom.plain}`) : [];
    const set = (patch: Partial<typeof custom>) => dispatch({ type: "signature_custom", template: patch });

    // The menu takes focus, so where the caret was is noted when it opens and put back to insert there.
    function remember() {
        const el = document.activeElement;
        caret.current = null;
        if (!(el instanceof HTMLElement) || !wrap.current?.contains(el)) return;
        if (el instanceof HTMLTextAreaElement) {
            if (!el.readOnly) caret.current = { el, start: el.selectionStart, end: el.selectionEnd };
        } else if (el.isContentEditable) {
            const sel = window.getSelection();
            caret.current = { el, range: sel && sel.rangeCount > 0 ? sel.getRangeAt(0).cloneRange() : null };
        }
    }

    function insert(token: string) {
        const at = caret.current;
        caret.current = null;
        if (at?.el.isConnected) {
            at.el.focus();
            if ("start" in at) at.el.setSelectionRange(at.start, at.end);
            else if (at.range) {
                const sel = window.getSelection();
                sel?.removeAllRanges();
                sel?.addRange(at.range);
            }
            if (document.execCommand?.("insertText", false, token)) return;
        }
        const html = custom.html.trim() ? `${custom.html} ${token}` : token;
        set({ html, plain: custom.plainSync ? signatureToPlain(html) : custom.plain.trim() ? `${custom.plain} ${token}` : token });
    }

    return (
        <div className="px-3.5 py-3 border-t border-slate-100">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                <span className="text-[11px] text-slate-500">Signature</span>
                <Segmented<SignatureChoice>
                    label="Signature"
                    pillId="buy-signature-pill"
                    value={choice}
                    onChange={(c) => dispatch({ type: "signature", choice: c })}
                    options={[
                        { value: "default", label: "Default" },
                        { value: "none", label: "None" },
                        { value: "custom", label: "Custom" },
                    ]}
                />
                {choice === "custom" && (
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                onMouseDown={remember}
                                className="ml-auto h-7 px-2 rounded-md border border-slate-200 bg-white hover:border-slate-300 text-[12px] font-medium text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 outline-none focus-visible:ring-2 focus-visible:ring-sky-200 transition-colors"
                            >
                                <BracesIcon className="w-3.5 h-3.5 text-slate-400" />
                                Insert placeholder
                                <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent aria-label="Insert placeholder" minWidth={240}>
                            {SIGNATURE_PLACEHOLDERS.map((p) => (
                                <PopoverMenuItem key={p.token} onSelect={() => insert(p.token)} description={<code className="font-mono">{p.token}</code>}>
                                    {p.label}
                                </PopoverMenuItem>
                            ))}
                        </PopoverMenuContent>
                    </PopoverMenu>
                )}
            </div>

            <AnimatePresence initial={false} mode="wait">
                <motion.div
                    key={choice}
                    initial={reduced ? { opacity: 0 } : { opacity: 0, y: -2 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15, ease: EASE }}
                    className="pt-2 space-y-2"
                >
                    {choice === "none" ? (
                        <p className="text-[11px] text-slate-500 leading-snug">The new mailboxes send without a signature. You can add one later in each mailbox&apos;s settings.</p>
                    ) : (
                        <>
                            {choice === "custom" && (
                                <>
                                    <div ref={wrap} className="overflow-x-auto">
                                        <EmailEditor
                                            id="buy-signature"
                                            htmlText={custom.html}
                                            setHtmlText={(html) => set({ html })}
                                            plainText={custom.plain}
                                            setPlainText={(plain) => set({ plain })}
                                            sync={custom.plainSync}
                                            setSync={(plainSync) => set({ plainSync })}
                                            code={code}
                                            setCode={setCode}
                                        />
                                    </div>
                                    <p className="text-[11px] text-slate-400 leading-relaxed">
                                        The same sender fields a bulk signature uses, filled in for each mailbox when you pay. Contact fields and spintax are not filled in inside a signature.
                                    </p>
                                    <UnknownPlaceholders unknown={unknown} />
                                </>
                            )}
                            <div>
                                <span className="block mb-1 text-[11px] text-slate-400">
                                    {choice === "default" ? "The sender's full name. Preview for " : "Preview for "}
                                    <span className="text-slate-600">{preview.address}</span>
                                </span>
                                <div aria-label={`Signature preview for ${preview.address}`} className="rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-1.5 min-h-9">
                                    {filled.html.trim() || filled.plain.trim() ? (
                                        <EmailBody html={filled.html} plain={filled.plain} />
                                    ) : (
                                        <p className="text-[11.5px] italic text-slate-400">Nothing to preview yet.</p>
                                    )}
                                </div>
                            </div>
                            <p className="text-[11px] text-slate-400 leading-snug">
                                Set in Warmbly and on the Google account. Microsoft 365 has no signature API, so those sign in Warmbly only. Edit it later in the mailbox&apos;s settings.
                            </p>
                        </>
                    )}
                </motion.div>
            </AnimatePresence>
        </div>
    );
}
