import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { KeyRound } from "lucide-react";
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
} from "@/components/ui/dialog";
import { Loading } from "@/components/loader";
import {
    registerPasskey,
    passkeySupported,
    platformPasskeyAvailable,
    PasskeyCancelled,
    SUGGEST_PASSKEY_FLAG,
} from "@/lib/passkey";
import listPasskeys from "@/lib/api/client/auth/passkey/listCredentials";

// Persisted "don't ask again" — set only on an explicit "Not now", so a casual
// X/escape stays a soft dismiss.
const DISMISS_KEY = "warmbly:passkey-nudge-dismissed";

/**
 * One-time, post-sign-in nudge to enroll a passkey. Shown only when the
 * password flow set the session flag, the device has a platform authenticator,
 * and the account has no passkeys yet. Never interrupts the sign-in itself.
 */
export default function PasskeyEnrollPrompt() {
    const [open, setOpen] = useState(false);
    const [busy, setBusy] = useState(false);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                if (sessionStorage.getItem(SUGGEST_PASSKEY_FLAG) !== "1") return;
                sessionStorage.removeItem(SUGGEST_PASSKEY_FLAG);
                if (localStorage.getItem(DISMISS_KEY) === "1") return;
                if (!passkeySupported() || !(await platformPasskeyAvailable())) return;

                const existing = await listPasskeys();
                if (!cancelled && existing.length === 0) setOpen(true);
            } catch {
                /* never block the dashboard on the nudge */
            }
        })();
        return () => {
            cancelled = true;
        };
    }, []);

    const handleCreate = async () => {
        setBusy(true);
        try {
            await registerPasskey();
            toast.success("Passkey added — use it to sign in next time.");
            setOpen(false);
        } catch (e) {
            if (!(e instanceof PasskeyCancelled)) {
                toast.error((e as Error)?.message || "Couldn't create a passkey.");
            }
        } finally {
            setBusy(false);
        }
    };

    const dismissForever = () => {
        try {
            localStorage.setItem(DISMISS_KEY, "1");
        } catch {
            /* storage unavailable */
        }
        setOpen(false);
    };

    return (
        <Dialog open={open} onOpenChange={(o) => { if (!o) setOpen(false); }}>
            <DialogContent className="sm:max-w-md bg-white">
                <DialogHeader>
                    <div className="mx-auto sm:mx-0 w-12 h-12 rounded-2xl bg-sky-50 flex items-center justify-center mb-1">
                        <KeyRound className="w-6 h-6 text-sky-500" />
                    </div>
                    <DialogTitle>Sign in faster with a passkey</DialogTitle>
                    <DialogDescription>
                        Next time, use Touch ID, Face ID, or your device PIN to sign in instantly — no password,
                        no email code. Your passkey stays on your device.
                    </DialogDescription>
                </DialogHeader>
                <DialogFooter className="mt-2">
                    <button
                        type="button"
                        onClick={dismissForever}
                        className="h-9 px-3 rounded-md text-[13px] font-medium text-slate-500 hover:bg-slate-100 transition-colors"
                    >
                        Not now
                    </button>
                    <button
                        type="button"
                        onClick={handleCreate}
                        disabled={busy}
                        className="h-9 px-4 rounded-md text-[13px] font-semibold text-white bg-gradient-to-b from-sky-500 to-sky-600 hover:from-sky-500 hover:to-sky-700 shadow-sm inline-flex items-center justify-center gap-2 disabled:opacity-60 disabled:pointer-events-none"
                    >
                        {busy && <Loading className="!w-4 h-4 text-white" />}
                        Set up passkey
                    </button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
