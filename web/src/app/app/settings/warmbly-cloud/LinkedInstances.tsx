// Cloud side of the same page: the self-hosted instances linked to this
// workspace and the pool allowance they share.

import React from "react";
import { Navigate } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import { Loader2Icon, ServerIcon } from "lucide-react";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { usePoolLinkInstances, useRevokePoolLinkInstance } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { useConfirm } from "@/hooks/context/confirm";
import { TableSurface } from "../_components/SectionShell";

export default function LinkedInstances() {
    const q = usePoolLinkInstances();
    const revoke = useRevokePoolLinkInstance();
    const confirm = useConfirm();

    if (q.isLoading) {
        return (
            <div className="py-8 flex justify-center text-slate-400">
                <Loader2Icon className="w-4 h-4 animate-spin" />
            </div>
        );
    }
    const list = q.data?.data ?? [];
    const plan = q.data?.plan;
    // Linking is closed, so a workspace with no live link has nothing to manage here.
    if (q.isSuccess && !list.some((i) => !i.revoked_at)) return <Navigate to="/app/settings/profile" replace />;

    return (
        <div className="space-y-3">
            {plan && (
                <p className="text-[12.5px] text-slate-500">
                    {plan.mailbox_limit === null
                        ? `Unlimited mailboxes · ${plan.enrolled} warming`
                        : plan.subscription_required
                          ? "Linked instances keep the mailboxes they already enrolled. Adding mailboxes through one needs a paid plan."
                          : `${plan.enrolled} of ${plan.mailbox_limit} free mailboxes used, counting the ones connected here and through instances`}
                </p>
            )}
            <TableSurface>
                <ul className="divide-y divide-slate-200/70">
                    {list.map((inst) => (
                        <li key={inst.id} className="flex items-center gap-3 px-3 h-12 bg-white">
                            <span className="size-7 rounded-md bg-slate-100 text-slate-600 inline-flex items-center justify-center shrink-0">
                                <ServerIcon className="w-3.5 h-3.5" />
                            </span>
                            <div className="min-w-0 flex-1">
                                <p className="text-[12.5px] text-slate-900 truncate">{inst.name}</p>
                                <p className="text-[11px] text-slate-400 truncate">
                                    {inst.mailbox_count} mailbox{inst.mailbox_count === 1 ? "" : "es"}
                                    {inst.version && ` · v${inst.version}`}
                                    {inst.last_seen_at && ` · seen ${new Date(inst.last_seen_at).toLocaleString()}`}
                                </p>
                            </div>
                            <button
                                type="button"
                                onClick={() =>
                                    confirm.show(`Unlink ${inst.name}? Its ${inst.mailbox_count} mailbox links are released. SMTP/IMAP Cloud enrollments are deleted. Cloud warmup sending and receiving stop for every linked mailbox. Cloud-managed mailboxes stay in your Cloud workspace with their settings and history, and you can restart their warmup there.`, async () => {
                                        try {
                                            await revoke.mutateAsync(inst.id);
                                            toast.success("Instance unlinked");
                                        } catch (e) {
                                            toast.error(buildError(e as AppError));
                                        }
                                    })
                                }
                                className="h-7 px-2.5 rounded-md text-[12px] text-rose-600 hover:bg-rose-50 transition-colors"
                            >
                                Unlink
                            </button>
                        </li>
                    ))}
                </ul>
            </TableSurface>
        </div>
    );
}
