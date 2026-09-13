// Public-beta notice.
//
// Two surfaces, because they answer different questions:
//
//   - a dialog on first visit, once per browser, for "what am I looking at
//     and what should I expect"
//   - a small pill in the header afterwards, for "wait, was this the beta?"
//     asked three days later, or asked by a teammate who joined after the
//     dialog was dismissed. Clicking it brings the explanation back.
//
// Driven by WARMBLY_BETA_NOTICE, not by hostname: one built image is served on
// whatever host the operator points at it, so matching "dev.warmbly.com" would
// both miss a rename and follow the image into production. Unset (the default)
// renders nothing at all, so production needs no one to remember to turn it
// off.

import { useCallback, useEffect, useState } from "react";
import { FlaskConicalIcon } from "lucide-react";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { BETA_NOTICE } from "@/lib/information";

// Keyed on the message, so rewording it shows the dialog again rather than
// leaving everyone dismissed on text they never read.
function seenKey(notice: string): string {
    let hash = 0;
    for (let i = 0; i < notice.length; i++) hash = (hash * 31 + notice.charCodeAt(i)) | 0;
    return `warmbly.beta-seen.${hash}`;
}

export function BetaPill() {
    const [open, setOpen] = useState(false);

    useEffect(() => {
        if (!BETA_NOTICE) return;
        try {
            if (window.localStorage.getItem(seenKey(BETA_NOTICE)) !== "1") setOpen(true);
        } catch {
            // Storage blocked (private mode, strict settings). Showing it every
            // load is the safer failure: the point is that nobody is surprised.
            setOpen(true);
        }
    }, []);

    const acknowledge = useCallback(() => {
        setOpen(false);
        try {
            if (BETA_NOTICE) window.localStorage.setItem(seenKey(BETA_NOTICE), "1");
        } catch {
            // Nothing to do; it reappears next load.
        }
    }, []);

    if (!BETA_NOTICE) return null;

    return (
        <>
            <button
                type="button"
                onClick={() => setOpen(true)}
                title="This deployment is a public beta. Click for details."
                className="inline-flex items-center gap-1 h-6 px-1.5 rounded border border-amber-200 bg-amber-50 text-amber-700 text-[11px] font-medium hover:bg-amber-100 transition-colors focus:outline-none focus:ring-2 focus:ring-amber-300"
            >
                <FlaskConicalIcon className="size-3" aria-hidden="true" />
                Beta
            </button>

            {/* onOpenChange rather than a close-only handler, so Escape and the
                backdrop record the acknowledgement too. Dismissing any way at
                all means it was read. */}
            <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : acknowledge())}>
                <DialogContent className="sm:max-w-[440px]">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <FlaskConicalIcon className="size-4 text-amber-600" aria-hidden="true" />
                            You are on the public beta
                        </DialogTitle>
                        <DialogDescription>{BETA_NOTICE}</DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button onClick={acknowledge} className="h-8">
                            Got it
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}

export default BetaPill;
