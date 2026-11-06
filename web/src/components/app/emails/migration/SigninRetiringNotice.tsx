// The mailbox drawer's optional alternatives to the older per-mailbox Google
// sign-in: reconnect with the new Google sign-in once the Workspace admin
// trusts Warmbly, switch to an app password, or move onto the domain's
// existing admin grant where one is already there.
import React from "react";
import toast from "react-hot-toast/headless";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowRightIcon, KeyRoundIcon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { MoveError, useGrantConfig, useMoveToGrant, useSigninMigration } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import MailboxImportDialog from "../import/MailboxImportDialog";
import { BlockedByAdminHint, TrustedAppSteps } from "../GoogleWorkspaceSigninPanel";
import AppPasswordSwitchForm from "./AppPasswordSwitchForm";
import { migrationRoute } from "./migrationRoute";
import useGoogleReconnect from "./useGoogleReconnect";

export default function SigninRetiringNotice({ mailbox }: { mailbox: { id: string; email: string } }) {
    const migration = useSigninMigration();
    const clientId = useGrantConfig().data?.google_mailbox_client_id;
    const move = useMoveToGrant();
    const google = useGoogleReconnect();
    const [importId, setImportId] = React.useState<string | null>(null);
    const [appPassword, setAppPassword] = React.useState(false);

    const group = migration.data?.data.find((g) => g.mailboxes.some((m) => m.id === mailbox.id));
    // The dialog stays mounted after the move, while the notice itself goes away.
    const dialog = <MailboxImportDialog importId={importId} onClose={() => setImportId(null)} />;
    if (!group || google.done.has(mailbox.id)) return dialog;
    const route = migrationRoute(group);

    async function moveIt() {
        if (!group?.grant_id || move.isPending) return;
        try {
            const out = await move.mutateAsync({ grantId: group.grant_id, emails: [mailbox.email] });
            toast.success(`Moving ${mailbox.email} onto the admin grant`);
            setImportId(out.job.id);
        } catch (e) {
            toast.error(e instanceof MoveError ? e.message : buildError(e as AppError));
        }
    }

    const reconnecting = google.busy === mailbox.id;

    return (
        <div className="px-5 py-4">
            <div className="rounded-md border border-amber-200 bg-amber-50/70 px-3 py-2.5 space-y-2.5">
                <div className="flex items-start gap-2.5">
                    <ProviderLogo id="google" size="sm" className="mt-px" />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12.5px] font-medium text-amber-900">Other connection methods</p>
                        <p className="text-[11.5px] text-amber-800 leading-relaxed">
                            {route === "app_password"
                                ? "This personal Gmail mailbox uses the older Google sign-in. You can keep it, or switch to an app password. Switching preserves its history, campaigns and warmup."
                                : "This mailbox uses the older Google sign-in. You can keep it, or reconnect it with the new Google sign-in, which moves its warmup engagement to IMAP. Reconnecting preserves its history, campaigns and warmup."}
                        </p>
                    </div>
                </div>

                {route === "signin" && (
                    <>
                        <div className="rounded-md border border-amber-200/80 bg-white px-3 py-2.5 space-y-2">
                            <p className="text-[12px] font-medium text-slate-900">First, your Workspace admin trusts Warmbly, once per domain</p>
                            <TrustedAppSteps clientId={clientId} />
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                            <button
                                type="button"
                                onClick={() => void google.reconnect(mailbox.id)}
                                aria-disabled={!!google.busy}
                                className={cn(
                                    "h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                                    google.busy && "opacity-60",
                                )}
                            >
                                {reconnecting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCwIcon className="w-3 h-3" />}
                                {reconnecting ? "Waiting for authorization…" : "Reconnect with Google"}
                            </button>
                            <button
                                type="button"
                                onClick={() => setAppPassword((v) => !v)}
                                aria-expanded={appPassword}
                                className={cn(
                                    "h-7 px-2.5 rounded-md border text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                                    appPassword ? "border-sky-200 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50",
                                )}
                            >
                                <KeyRoundIcon className="w-3 h-3" />
                                Switch to an app password
                            </button>
                        </div>
                        {google.unfinished === mailbox.id && <BlockedByAdminHint clientId={clientId} />}
                        <AnimatePresence initial={false}>
                            {appPassword && (
                                <motion.div
                                    key="app-password"
                                    initial={{ opacity: 0, height: 0 }}
                                    animate={{ opacity: 1, height: "auto" }}
                                    exit={{ opacity: 0, height: 0 }}
                                    transition={{ duration: 0.18 }}
                                    className="overflow-hidden"
                                >
                                    <AppPasswordSwitchForm mailbox={mailbox} />
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </>
                )}
                {route === "grant" && (
                    <button
                        type="button"
                        onClick={() => void moveIt()}
                        aria-disabled={move.isPending}
                        className={cn(
                            "h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                            move.isPending && "opacity-60",
                        )}
                    >
                        {move.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ArrowRightIcon className="w-3 h-3" />}
                        Move to the existing admin grant for {group.domain}
                    </button>
                )}
                {route === "app_password" && <AppPasswordSwitchForm mailbox={mailbox} />}
            </div>
            {dialog}
        </div>
    );
}
