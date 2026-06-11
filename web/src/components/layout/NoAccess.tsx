// NoAccess — an explicit "you don't have access" state for permission-gated
// pages. Shown instead of a blank or misleading-empty page when the current
// member lacks the permission a feature requires, so the app always says WHY
// something is unavailable rather than rendering nothing.

import { Link } from "react-router-dom";
import { LockIcon } from "lucide-react";

export function NoAccess({
    feature,
    permissionLabel,
}: {
    /** Human name of the area, e.g. "the unified inbox". */
    feature: string;
    /** The permission a teammate would need, e.g. "Use unified inbox". */
    permissionLabel: string;
}) {
    return (
        <div className="flex-1 min-h-[60vh] flex items-center justify-center px-6">
            <div className="max-w-sm text-center">
                <div className="mx-auto mb-4 size-11 rounded-xl bg-amber-50 border border-amber-200 text-amber-600 flex items-center justify-center">
                    <LockIcon className="w-5 h-5" />
                </div>
                <h2 className="text-[15px] font-semibold text-slate-900">
                    You don't have access to {feature}
                </h2>
                <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1.5">
                    Your role in this workspace doesn't include the{" "}
                    <span className="font-medium text-slate-700">{permissionLabel}</span>{" "}
                    permission. Ask a workspace admin or the owner to grant it from{" "}
                    <span className="font-medium text-slate-700">Settings → Roles &amp; access</span>.
                </p>
                <Link
                    to="/app"
                    className="mt-4 inline-flex items-center h-8 px-3 rounded-md border border-slate-200 bg-white hover:bg-slate-50 text-[12.5px] font-medium text-slate-700 transition-colors"
                >
                    Back to dashboard
                </Link>
            </div>
        </div>
    );
}
