// Mobile scope picker.
//
// The desktop ScopeRail is `hidden lg:flex`, which means phones and
// most tablets land in the inbox with no way to switch between
// Inbox / Unread / Today / Awaiting / Snoozed / Scheduled / mailboxes
// / tags. This sheet surfaces the same ScopeRail in a slide-over,
// dismissed by tapping the backdrop, the close handle, or by
// picking any scope (which is what the user came in for).

import { AnimatePresence, motion } from "framer-motion";
import { XIcon } from "lucide-react";
import { ScopeRail, type UniboxScope } from "./ScopeRail";

interface ScopeSheetProps {
    open: boolean;
    setOpen: (o: boolean) => void;
    scope: UniboxScope;
    onChange: (s: UniboxScope) => void;
}

export function ScopeSheet({ open, setOpen, scope, onChange }: ScopeSheetProps) {
    const pick = (s: UniboxScope) => {
        onChange(s);
        setOpen(false);
    };

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.18 }}
                    onClick={() => setOpen(false)}
                    className="fixed inset-0 z-[100] flex bg-slate-900/30 backdrop-blur-[2px] lg:hidden"
                >
                    <motion.aside
                        key="panel"
                        initial={{ x: "-100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "-100%" }}
                        transition={{ type: "spring", stiffness: 300, damping: 32 }}
                        onClick={(e) => e.stopPropagation()}
                        className="flex flex-col bg-white w-[280px] max-w-[88%] h-full border-r border-slate-200 shadow-[8px_0_24px_-12px_rgba(15,23,42,0.18)]"
                    >
                        <div className="h-11 px-4 border-b border-slate-200 flex items-center gap-3 shrink-0">
                            <span className="text-[13.5px] font-semibold text-slate-900">Views</span>
                            <button
                                type="button"
                                onClick={() => setOpen(false)}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                        <div className="flex-1 min-h-0">
                            <ScopeRail scope={scope} onChange={pick} />
                        </div>
                    </motion.aside>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
