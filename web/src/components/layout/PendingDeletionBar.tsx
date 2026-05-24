// App-wide banner shown when the current workspace (or the user's own
// account) is scheduled for deletion. Sits above AppHeader so it's
// impossible to miss while clicking around the dashboard.
//
// Two banners can stack:
//   1. Workspace pending deletion (anyone in the org sees this)
//   2. Personal account pending deletion (only the user themselves)
//
// We deliberately don't make these dismissible — they need to nag.

import React from "react";
import { Link } from "react-router-dom";
import { AlertOctagonIcon, Loader2Icon, UndoIcon } from "lucide-react";
import toast from "react-hot-toast";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useOrganizationDangerZone from "@/lib/api/hooks/app/dangerzone/useOrganizationDangerZone";
import useAccountDangerZone from "@/lib/api/hooks/app/dangerzone/useAccountDangerZone";
import useCancelOrganizationDeletion from "@/lib/api/hooks/app/dangerzone/useCancelOrganizationDeletion";
import useCancelAccountDeletion from "@/lib/api/hooks/app/dangerzone/useCancelAccountDeletion";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";

// Re-render every minute so "X days Y hours" stays honest on a tab
// that's been open for hours. Cheap — single setState per banner.
function useMinuteTick() {
    const [, setTick] = React.useState(0);
    React.useEffect(() => {
        const id = window.setInterval(() => setTick((t) => t + 1), 60_000);
        return () => window.clearInterval(id);
    }, []);
}

function formatRemaining(executeAfter: Date): string {
    const ms = executeAfter.getTime() - Date.now();
    if (ms <= 0) return "any moment now";
    const totalMinutes = Math.floor(ms / 60_000);
    const days = Math.floor(totalMinutes / (60 * 24));
    const hours = Math.floor((totalMinutes % (60 * 24)) / 60);
    const minutes = totalMinutes % 60;
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
}

export default function PendingDeletionBar() {
    useMinuteTick();

    const access = useFeatureAccess();
    const org = useOrganizationDangerZone();
    const account = useAccountDangerZone();

    const cancelOrg = useCancelOrganizationDeletion();
    const cancelAccount = useCancelAccountDeletion();

    const orgPending = org.data?.pending_deletion;
    const accountPending = account.data?.pending_deletion;

    if (!orgPending && !accountPending) return null;

    return (
        <div className="relative z-20">
            {orgPending && org.data && (
                <Banner
                    title={
                        <>
                            This workspace will be permanently deleted in{" "}
                            <strong className="tabular-nums">
                                {formatRemaining(new Date(orgPending.execute_after))}
                            </strong>
                            .
                        </>
                    }
                    detail={
                        <>
                            <strong>{org.data.resource_name}</strong> is scheduled
                            for deletion on{" "}
                            {new Date(orgPending.execute_after).toLocaleString()}.
                            {access.isOwner
                                ? " Cancel below to keep it."
                                : " Only the workspace owner can cancel — reach out to them now."}
                        </>
                    }
                    showCancel={access.isOwner}
                    onCancel={async () => {
                        try {
                            await cancelOrg.mutateAsync(undefined);
                            toast.success("Workspace deletion cancelled");
                        } catch (err) {
                            toast.error(buildError(err as AppError));
                        }
                    }}
                    cancelLoading={cancelOrg.isPending}
                />
            )}

            {accountPending && account.data && (
                <Banner
                    title={
                        <>
                            Your account will be permanently deleted in{" "}
                            <strong className="tabular-nums">
                                {formatRemaining(new Date(accountPending.execute_after))}
                            </strong>
                            .
                        </>
                    }
                    detail={
                        <>
                            All workspaces you own and every campaign / contact /
                            mailbox in them will be removed on{" "}
                            {new Date(accountPending.execute_after).toLocaleString()}.
                        </>
                    }
                    showCancel={true}
                    onCancel={async () => {
                        try {
                            await cancelAccount.mutateAsync(undefined);
                            toast.success("Account deletion cancelled");
                        } catch (err) {
                            toast.error(buildError(err as AppError));
                        }
                    }}
                    cancelLoading={cancelAccount.isPending}
                />
            )}
        </div>
    );
}

function Banner({
    title,
    detail,
    showCancel,
    onCancel,
    cancelLoading,
}: {
    title: React.ReactNode;
    detail: React.ReactNode;
    showCancel: boolean;
    onCancel: () => void | Promise<void>;
    cancelLoading: boolean;
}) {
    return (
        <div
            role="alert"
            className="bg-red-600 text-white px-4 py-2 flex items-center gap-3 border-b border-red-800/50 shadow-[0_1px_0_rgba(0,0,0,0.05)]"
        >
            <div className="size-5 rounded bg-white/15 flex items-center justify-center shrink-0">
                <AlertOctagonIcon className="w-3 h-3" />
            </div>
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] font-medium leading-tight">
                    {title}
                </div>
                <div className="text-[11.5px] text-white/85 leading-tight mt-0.5">
                    {detail}
                </div>
            </div>
            <div className="flex items-center gap-1.5 shrink-0">
                {showCancel && (
                    <button
                        type="button"
                        onClick={() => onCancel()}
                        disabled={cancelLoading}
                        className="h-7 px-2.5 rounded-md bg-white text-red-700 hover:bg-red-50 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-70"
                    >
                        {cancelLoading ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : (
                            <UndoIcon className="w-3 h-3" />
                        )}
                        Cancel deletion
                    </button>
                )}
                <Link
                    to="/app/settings/danger"
                    className="h-7 px-2.5 rounded-md bg-red-700 hover:bg-red-800 text-white text-[12px] font-medium inline-flex items-center transition-colors"
                >
                    Settings
                </Link>
            </div>
        </div>
    );
}
