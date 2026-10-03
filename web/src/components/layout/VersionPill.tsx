// VersionPill: the running Warmbly version, for self-hosted instances only.
//
// Everyone in the workspace sees it, so nobody has to ask which version the
// server runs, and it turns amber when a newer release exists. Only a platform
// admin can act on it: for them it opens the update dialog, follows a running
// update with a spinner, and reports the outcome once the backend is back.
// For everyone else it is a badge whose tooltip says who to ask. Hosted
// deployments never render it.

import React from "react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowUpCircleIcon, Loader2Icon } from "lucide-react";
import useInstanceVersion from "@/lib/api/hooks/auth/useInstanceVersion";
import useUser from "@/lib/api/hooks/auth/useUser";
import { showAdminMFARequired } from "@/hooks/usePermission";
import { isUpdateRunning, runningLabel, useInstanceUpdate } from "@/lib/api/hooks/auth/useInstanceUpdate";
import { clearUpdateStarted, readUpdateStarted } from "@/lib/updateSession";
import { cn } from "@/lib/utils";
import UpdateDialog from "./UpdateDialog";

// Platform admin permission bits, mirroring internal/models/admin_permission.go.
const ADMIN_VIEW_ANALYTICS = 1 << 11;
const ADMIN_MANAGE_SETTINGS = 1 << 14;

export function VersionPill() {
    const versionQ = useInstanceVersion();
    const { data: user } = useUser();
    // Mirrors the backend gates: view_analytics reads the update state,
    // manage_settings is what check and apply require, and every admin route
    // refuses a session that never presented a second factor. An admin without
    // the latter sees the same read-only badge as a member; one whose session
    // lacks 2FA gets told so on click instead of a refused request.
    const perms = user?.admin_permissions ?? 0;
    const mfaOK = user?.session_mfa_verified === true;
    const holdsAdmin = (perms & ADMIN_MANAGE_SETTINGS) === ADMIN_MANAGE_SETTINGS;
    const canView = mfaOK && (perms & ADMIN_VIEW_ANALYTICS) === ADMIN_VIEW_ANALYTICS;
    const isAdmin = mfaOK && holdsAdmin;
    const needsMFA = holdsAdmin && !mfaOK;
    const v = versionQ.data;
    const selfHosted = !!v?.self_hosted;

    const adminQ = useInstanceUpdate(canView && selfHosted);
    const admin = adminQ.data;
    const qc = useQueryClient();
    const [open, setOpen] = React.useState(false);

    const started = readUpdateStarted();
    const updating = isUpdateRunning(admin);
    const restarting = !!started && (adminQ.isError || versionQ.isError);

    // The backend is back after an update this browser started: say so once,
    // then refresh everything so no page keeps stale data. The dialog handles
    // the reload itself when it is open.
    React.useEffect(() => {
        if (!started || !admin || updating || open) return;
        const last = admin?.updater?.last_job;
        const moved =
            (admin.running?.commit && admin.running.commit !== started.fromCommit) ||
            (admin.running?.version && admin.running.version !== started.fromVersion);
        if (last?.status === "failed") {
            clearUpdateStarted();
            toast.error(`The update failed: ${last.error ?? "open the version pill for the log"}`);
        } else if (moved || last?.status === "succeeded") {
            clearUpdateStarted();
            toast.success(`Updated to ${runningLabel(admin)}`);
            void qc.invalidateQueries();
        }
    }, [started, admin, updating, open, qc]);

    if (!v || !selfHosted) return null;

    const running = v.version && v.version !== "dev" ? v.version : v.commit ? `dev ${v.commit.slice(0, 7)}` : "dev";
    const available = v.update_available;
    const latest = v.latest?.tag;

    let label = available ? (latest ? `Update to ${latest}` : "Update available") : running;
    let icon: React.ReactNode = available ? (
        <span className="relative flex size-2.5 items-center justify-center">
            <span className="absolute inline-flex size-full rounded-full bg-amber-400 opacity-60 animate-ping" />
            <ArrowUpCircleIcon className="relative w-3 h-3" />
        </span>
    ) : (
        <span className="size-1.5 rounded-full bg-slate-400" />
    );
    let tone = available ? "bg-amber-50 text-amber-800 border-amber-200" : "bg-white/70 text-slate-500 border-slate-200";
    let title = available
        ? isAdmin
            ? `Warmbly ${latest ?? "newer"} is available; this instance runs ${running}. Click to update.`
            : needsMFA
              ? `Warmbly ${latest ?? "newer"} is available; this instance runs ${running}. Updating needs a session with two-factor authentication.`
              : `Warmbly ${latest ?? "newer"} is available; this instance runs ${running}. Ask a platform admin to update.`
        : isAdmin
          ? `Warmbly ${running}, up to date. Click for details.`
          : needsMFA
            ? `Warmbly ${running}, up to date. Update details need a session with two-factor authentication.`
            : `Warmbly ${running}, up to date.`;

    if (isAdmin && (updating || restarting)) {
        label = restarting ? "Reconnecting" : "Updating";
        icon = <Loader2Icon className="w-3 h-3 animate-spin" />;
        tone = "bg-sky-50 text-sky-700 border-sky-200";
        title = restarting ? "The backend is restarting after an update" : `Update in progress: ${admin?.updater?.job?.step ?? ""}`;
    }

    const className = cn(
        "inline-flex items-center gap-1.5 h-6 px-2 rounded border text-[11px] font-semibold tracking-[0.02em] transition-colors",
        tone,
        (isAdmin || needsMFA) && (available ? "hover:bg-amber-100" : "hover:text-slate-900 hover:bg-white"),
    );

    if (needsMFA) {
        return (
            <button type="button" onClick={showAdminMFARequired} title={title} className={className}>
                {icon}
                {label}
            </button>
        );
    }
    if (!isAdmin) {
        return (
            <span title={title} className={cn(className, "cursor-default")}>
                {icon}
                {label}
            </span>
        );
    }
    return (
        <>
            <button type="button" onClick={() => setOpen(true)} title={title} className={className}>
                {icon}
                {label}
            </button>
            <UpdateDialog open={open} onClose={() => setOpen(false)} />
        </>
    );
}
