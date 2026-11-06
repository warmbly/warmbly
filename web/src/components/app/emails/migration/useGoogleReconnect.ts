// useGoogleReconnect: renews a Google sign-in mailbox's consent in place, the
// move off the older Google sign-in once the Workspace admin trusts Warmbly.
import React from "react";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";
import reauthEmailOAuth from "@/lib/api/client/app/emails/reauthEmailOAuth";
import onboardOAuthFinish from "@/lib/api/client/app/emails/onboardOAuthFinish";
import { authorizeEmailInPopup } from "@/lib/emails/emailOAuthPopup";
import { SIGNIN_MIGRATION_KEY } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { MAILBOX_SIGNIN_KEY } from "@/lib/api/hooks/app/emails/useMailboxSignin";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function useGoogleReconnect() {
    const qc = useQueryClient();
    const [busy, setBusy] = React.useState<string | null>(null);
    // The last reconnect that did not finish, so the caller can explain a Workspace block.
    const [unfinished, setUnfinished] = React.useState<string | null>(null);
    const [done, setDone] = React.useState<Set<string>>(() => new Set());

    // Call it straight from the click: the window opens before the start request.
    const reconnect = React.useCallback(
        async (mailboxId: string) => {
            if (busy) return;
            setBusy(mailboxId);
            setUnfinished(null);
            try {
                const { code, state } = await authorizeEmailInPopup(() => reauthEmailOAuth(mailboxId));
                await onboardOAuthFinish(code, state);
                toast.success("Reconnected with the new Google sign-in");
                setDone((prev) => new Set(prev).add(mailboxId));
                qc.invalidateQueries({ queryKey: ["emails", "list"] });
                qc.invalidateQueries({ queryKey: MAILBOX_SIGNIN_KEY });
                qc.invalidateQueries({ queryKey: SIGNIN_MIGRATION_KEY });
            } catch (e) {
                setUnfinished(mailboxId);
                toast.error(e instanceof Error ? e.message : buildError(e as AppError));
            } finally {
                setBusy(null);
            }
        },
        [busy, qc],
    );

    return { busy, unfinished, done, reconnect };
}
