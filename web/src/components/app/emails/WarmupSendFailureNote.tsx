// Why a warming mailbox is sending nothing: its last warmup email failed and
// no later one was confirmed delivered.

import { AlertTriangleIcon } from "lucide-react";
import type { WarmupSendFailure } from "@/lib/api/models/app/analytics/AccountStatus";

export default function WarmupSendFailureNote({ failure, provider, cloud = false, className = "" }: { failure: WarmupSendFailure; provider?: string; cloud?: boolean; className?: string }) {
    const advice = provider === "smtp_imap"
        ? cloud
            ? "If the error above reports a mail-server connection problem, check the SMTP host, port and any IP allowlist. Warmbly Cloud connects from its own network, not this instance's address."
            : "If the error above reports a mail-server connection problem, check the SMTP host, port and password in the mailbox's settings."
        : provider === "gmail" || provider === "outlook"
            ? "This mailbox uses the provider's API, not SMTP. Temporary worker or provider failures are retried automatically; reconnect only if the provider reports a sign-in or permission error."
            : "Temporary worker or provider failures are retried automatically. Follow any provider-specific error shown above; a worker-loading failure alone does not mean the credentials are wrong.";
    return (
        <div className={`mt-2 flex items-start gap-1.5 text-[11.5px] leading-relaxed text-rose-700 ${className}`}>
            <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
            <div className="min-w-0">
                <div>
                    {cloud ? "Warmbly Cloud could not send the last warmup email from this mailbox." : "The last warmup email from this mailbox was not sent."} None has been delivered since, so
                    today's count stays where it is.
                </div>
                <div className="mt-0.5 font-mono text-[11px] text-rose-600/90 break-words">{failure.message}</div>
                <div className="mt-0.5 text-slate-500">
                    {advice}
                </div>
            </div>
        </div>
    );
}
