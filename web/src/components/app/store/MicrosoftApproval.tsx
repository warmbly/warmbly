// The step a person takes for purchased Microsoft 365 mailboxes only when Warmbly could not approve itself with the
// domain's admin mailbox: the administrator approves Warmbly once, in Microsoft, and lands back on this order.
import React from "react";
import toast from "react-hot-toast/headless";
import { AnimatePresence } from "framer-motion";
import { ArrowUpRightIcon, KeyRoundIcon, Loader2Icon, ShieldCheckIcon } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { usePermission } from "@/hooks/usePermission";
import { signInStoreMailbox, startStoreMicrosoftConsent } from "@/lib/api/client/app/store/store";
import { useStoreMailboxes } from "@/lib/api/hooks/app/store/useStore";
import type { MailboxSignIn, Order, StoreMailbox } from "@/lib/api/models/app/store/Store";
import { approvalReason, waitingForApproval, type Waiting } from "@/lib/buy/approval";
import { storeErrorMessage } from "@/lib/buy/errors";
import { PressButton } from "./Press";
import SignInDialog from "./SignInDialog";

const ADMIN_CONSOLE = "https://admin.microsoft.com";

export default function MicrosoftApproval({ order }: { order: Order }) {
    const waiting = waitingForApproval(order);
    // The admin mailbox may have been bought in an earlier order, so the workspace's list is read too.
    const mailboxes = useStoreMailboxes(waiting.length > 0).data?.data;
    if (waiting.length === 0) return null;
    const adminFor = (domainId: string): StoreMailbox | undefined =>
        order.mailboxes.find((m) => m.is_admin && m.domain_id === domainId) ?? mailboxes?.find((m) => m.is_admin && m.domain_id === domainId);
    return (
        <div className="mt-4 space-y-2.5">
            {waiting.map((w) => (
                <Card key={w.domainId} waiting={w} orderId={order.id} admin={adminFor(w.domainId)} />
            ))}
        </div>
    );
}

function Card({ waiting, orderId, admin }: { waiting: Waiting; orderId: string; admin: StoreMailbox | undefined }) {
    const queryClient = useQueryClient();
    const canManage = usePermission("MANAGE_EMAILS");
    const [opening, setOpening] = React.useState(false);
    // A fresh sign-in each time, held only while its dialog is open; never in the query cache.
    const [signIn, setSignIn] = React.useState<MailboxSignIn | null>(null);
    const [signingIn, setSigningIn] = React.useState(false);
    const adminLive = !!admin && (admin.status === "active" || admin.status === "cancelling");

    const approve = async () => {
        if (opening) return;
        setOpening(true);
        try {
            const link = await startStoreMicrosoftConsent(waiting.domainId, orderId);
            if (link.url) {
                window.location.assign(link.url);
                return;
            }
            toast.success(`Already approved. Connecting the mailboxes on ${waiting.domain}.`);
            void queryClient.invalidateQueries({ queryKey: ["store"] });
        } catch (e) {
            toast.error(storeErrorMessage(e));
        }
        setOpening(false);
    };

    const openSignIn = async () => {
        if (!admin || signingIn) return;
        setSigningIn(true);
        try {
            setSignIn(await signInStoreMailbox(admin.id));
        } catch (e) {
            toast.error(storeErrorMessage(e));
        } finally {
            setSigningIn(false);
        }
    };

    const boxes = waiting.count === 1 ? "the mailbox" : `the ${waiting.count} mailboxes`;
    return (
        <div className="rounded-lg border border-sky-200 bg-sky-50/50 px-3.5 py-3">
            <div className="flex items-start gap-2.5">
                <ShieldCheckIcon className="w-4 h-4 text-sky-600 mt-px shrink-0" />
                <div className="min-w-0 flex-1">
                    <div className="text-[12.5px] font-semibold text-slate-900">Approve in Microsoft 365</div>
                    <p className="mt-0.5 text-[12px] font-medium text-slate-800 leading-relaxed">{approvalReason(waiting.reason, waiting.domain, admin?.email)}</p>
                    <p className="mt-0.5 text-[12px] text-slate-600 leading-relaxed">
                        Approve Warmbly once for {waiting.domain} so {boxes} can connect. Sign in as{" "}
                        {admin ? <span className="font-medium text-slate-900 break-all">{admin.email}</span> : "the domain's admin mailbox"} when Microsoft
                        asks, then accept. You come back here when it is done.
                    </p>
                    <div className="mt-2.5 flex flex-wrap items-center gap-2">
                        <PressButton disabled={!canManage || opening} onClick={() => void approve()}>
                            {opening ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ArrowUpRightIcon className="w-3 h-3" />} Approve in Microsoft 365
                        </PressButton>
                        {adminLive ? (
                            <PressButton tone="secondary" disabled={!canManage || signingIn} onClick={() => void openSignIn()}>
                                {signingIn ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <KeyRoundIcon className="w-3 h-3" />} Admin sign-in
                            </PressButton>
                        ) : (
                            <a
                                href={ADMIN_CONSOLE}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Microsoft 365 admin center <ArrowUpRightIcon className="w-3 h-3" />
                            </a>
                        )}
                    </div>
                    {!canManage && <p className="mt-2 text-[11px] text-slate-500">Ask a member who can manage mailboxes to approve it.</p>}
                </div>
            </div>
            <AnimatePresence>{signIn && <SignInDialog signIn={signIn} admin onClose={() => setSignIn(null)} />}</AnimatePresence>
        </div>
    );
}
