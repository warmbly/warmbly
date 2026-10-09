import { AlertTriangleIcon } from "lucide-react";
import { useEffect, useState } from "react";
import type { WarmupSendFailure } from "@/lib/api/models/app/analytics/AccountStatus";

export default function WarmupSendFailureNote({ failure, provider, cloud = false, className = "" }: { failure: WarmupSendFailure; provider?: string; cloud?: boolean; className?: string }) {
    const loading = failure.kind === "mailbox_loading" || failure.message.startsWith("The sending worker has not loaded this mailbox yet");
    const [expiredFailureAt, setExpiredFailureAt] = useState<string | null>(null);
    const lifetime = (loading ? 1 : 24) * 60 * 60 * 1000;
    useEffect(() => {
        const delay = Date.parse(failure.at) + lifetime - Date.now();
        if (!Number.isFinite(delay) || delay <= 0) return;
        const timer = setTimeout(() => setExpiredFailureAt(failure.at), delay);
        return () => clearTimeout(timer);
    }, [lifetime, failure.at]);
    const last = Date.parse(failure.at);
    const now = Date.now();
    if (expiredFailureAt === failure.at || !Number.isFinite(last) || last <= now - lifetime || last > now) return null;
    if (loading) {
        const first = Date.parse(failure.first_failure_at ?? "");
        const hour = 60 * 60 * 1000;
        if (!Number.isFinite(first) || first > now - hour) return null;
        return (
            <div className={`mt-2 flex items-start gap-1.5 text-[11.5px] leading-relaxed text-rose-700 ${className}`}>
                <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                <div className="min-w-0">
                    <div>{cloud ? "Warmbly Cloud" : "Warmbly"} has repeatedly been unable to load this mailbox for warmup over at least an hour. No later warmup send has been confirmed.</div>
                    <div className="mt-0.5 text-slate-500">Active mailboxes are reloaded automatically. A worker-loading failure does not mean your credentials are wrong. If this continues, contact support with the last failure time: {new Date(failure.at).toLocaleString()}.</div>
                </div>
            </div>
        );
    }
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
                    {cloud ? "Warmbly Cloud reported a failed warmup send from this mailbox." : "Warmbly reported a failed warmup send from this mailbox."} No later successful warmup send has been confirmed.
                </div>
                <div className="mt-0.5 font-mono text-[11px] text-rose-600/90 break-words">{failure.message}</div>
                <div className="mt-0.5 text-slate-500">Last reported failure: {new Date(failure.at).toLocaleString()}.</div>
                <div className="mt-0.5 text-slate-500">
                    {advice}
                </div>
            </div>
        </div>
    );
}
