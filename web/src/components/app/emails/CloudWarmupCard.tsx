// The Warmbly Cloud block at the top of a mailbox's Warmup tab on a
// self-hosted instance: enrolled state with the cloud's numbers and
// pause/resume/remove, or the way in when it is not enrolled yet.

import React from "react";
import { Link } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import { CloudIcon, Loader2Icon, PauseIcon, PlayIcon } from "lucide-react";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useConfirm } from "@/hooks/context/confirm";
import useCloudPool from "@/hooks/useCloudPool";
import { useCloudLinkMailboxLifecycle, useEnrollCloudLinkMailbox, useUnenrollCloudLinkMailbox } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { providerSupported } from "@/app/app/settings/warmbly-cloud/providers";
import WarmupPartnerDiversity from "./WarmupPartnerDiversity";
import WarmupSendFailureNote from "./WarmupSendFailureNote";
import { cloudSendFailure, cloudWarmupPaused } from "@/lib/cloudWarmup";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { setCloudLinkParticipation } from "@/lib/api/client/app/cloudlink/cloudLink";
import type { DiagnosticParticipation } from "@/lib/api/models/app/cloudlink/CloudLink";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";

export default function CloudWarmupCard({ mailboxId, email, provider }: { mailboxId: string; email: string; provider: string }) {
    const pool = useCloudPool();
    const enroll = useEnrollCloudLinkMailbox();
    const unenroll = useUnenrollCloudLinkMailbox();
    const lifecycle = useCloudLinkMailboxLifecycle();
    const confirm = useConfirm();
    const qc = useQueryClient();
    const participation = useMutation({ mutationFn: (p: DiagnosticParticipation) => setCloudLinkParticipation(mailboxId, p), onSuccess: () => qc.invalidateQueries({ queryKey: ["cloud-link"] }) });
    const busy = enroll.isPending || unenroll.isPending || lifecycle.isPending || participation.isPending;

    if (!pool.manageable) return null;

    const run = async (fn: () => Promise<unknown>, ok: string) => {
        try {
            await fn();
            toast.success(ok);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    if (!pool.connected || (!pool.workspaceConnected && !pool.isEnrolled(mailboxId))) {
        return (
            <div className="px-5 py-3 flex items-center gap-2.5 text-[12px] text-slate-500">
                <CloudIcon className="w-3.5 h-3.5 text-sky-600 shrink-0" />
                <span className="min-w-0 flex-1">Warm this mailbox in the Warmbly pool instead, free for up to 10 mailboxes.</span>
                <Link to="/app/settings/warmbly-cloud" className="shrink-0 font-medium text-sky-700 hover:text-sky-900">
                    Connect Warmbly Cloud
                </Link>
            </div>
        );
    }

    const row = pool.rowFor(mailboxId);
    const cloud = row?.cloud;
    const sendFailure = cloudSendFailure(cloud);
    const supported = providerSupported(provider);

    if (!row?.enrolled) {
        return (
            <div className="px-5 py-3 flex items-center gap-2.5 text-[12px] text-slate-600">
                <CloudIcon className="w-3.5 h-3.5 text-sky-600 shrink-0" />
                <span className="min-w-0 flex-1">
                    {supported ? "Not in the Warmbly pool. Enrolling stops local warmup and lets the cloud warm it." : "This mailbox was signed in with this instance's own OAuth app, which the cloud cannot refresh. Remove it and add it again through Warmbly Cloud to warm it there."}
                </span>
                {supported && (
                    <button
                        type="button"
                        disabled={busy}
                        onClick={() => confirm.show("Authorize disclosed automated diagnostic sending and receiving in Warmbly Cloud? This is not organic engagement or proof of improved reputation.", () => void run(() => enroll.mutateAsync(mailboxId), `${email} enrolled for diagnostics`))}
                        className="shrink-0 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CloudIcon className="w-3 h-3" />}
                        Warm in Warmbly Cloud
                    </button>
                )}
            </div>
        );
    }

    const p = cloud?.participation;
    const paused = cloudWarmupPaused(cloud) || !!p && !p.send;
    const health = cloud?.health?.state;
    return (
        <div className="px-5 py-4">
            <div className="rounded-md border border-sky-200/70 bg-sky-50/50 px-3 py-3">
                <div className="flex items-center gap-2.5">
                    <span className="size-8 rounded-lg bg-sky-600 text-white inline-flex items-center justify-center shrink-0">
                        <CloudIcon className="w-4 h-4" />
                    </span>
                    <div className="min-w-0 flex-1">
                        <div className="text-[12.5px] font-medium text-slate-900">
                            {p?.mode === "off" ? "Diagnostics off in Warmbly Cloud" : paused ? "Diagnostic sending paused in Cloud" : row.managed ? "Signed in through Warmbly Cloud" : "Automated diagnostics in Warmbly Cloud"}
                        </div>
                        <div className="text-[11px] text-slate-500 truncate">
                            {cloud
                                ? `${cloud.sent_today} of ${cloud.warmup?.target_volume ?? cloud.settings.base} today · ${cloud.sent_7d} in 7 days${cloud.spam_placed_7d ? ` · ${cloud.spam_placed_7d} landed in spam` : ""}${health ? ` · ${health}` : ""}`
                                : "Waiting for the cloud to report"}
                        </div>
                    </div>
                    <div className="flex items-center gap-1.5 shrink-0">
                        {cloud && (
                            <button
                                type="button"
                                disabled={busy}
                                onClick={() => void run(() => lifecycle.mutateAsync({ id: mailboxId, action: paused ? "resume" : "pause" }), paused ? "Warmup resumed" : "Warmup paused")}
                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] font-medium text-slate-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {paused ? <PlayIcon className="w-3 h-3" /> : <PauseIcon className="w-3 h-3" />}
                                {paused ? "Resume" : "Pause"}
                            </button>
                        )}
                        <button
                            type="button"
                            disabled={busy}
                            onClick={() =>
                                confirm.show(
                                    row.managed
                                        ? `Remove ${email} from this instance? It stays in your Warmbly Cloud workspace, where its sign-in lives.`
                                        : `Stop warming ${email} in the Warmbly pool? The cloud deletes its credential right away and local warmup takes over.`,
                                    async () => {
                                        await run(() => unenroll.mutateAsync(mailboxId), row.managed ? `${email} removed from this instance` : `${email} removed from the pool`);
                                    },
                                )
                            }
                            className="h-7 px-2.5 rounded-md text-[12px] font-medium text-slate-600 hover:text-rose-600 hover:bg-rose-50 transition-colors disabled:opacity-60"
                        >
                            Remove
                        </button>
                    </div>
                </div>
                {sendFailure && <WarmupSendFailureNote failure={sendFailure} cloud className="pl-10" />}
                {p && <div className="mt-3 pl-10 space-y-2 text-xs">
                    <label className="flex justify-between">Allow disclosed diagnostic sending<Toggle disabled={busy} value={p.send} onChange={(send) => {
                        const apply = () => void run(() => participation.mutateAsync({ ...p, mode: "diagnostic", send }), "Diagnostic sending updated");
                        if (send) confirm.show("Authorize automated diagnostic sends and replies? Provider policies and safety holds still apply.", apply); else apply();
                    }} /></label>
                    <label className="flex justify-between">Allow receiving diagnostic tests<Toggle disabled={busy} value={p.receive} onChange={(receive) => void run(() => participation.mutateAsync({ ...p, mode: "diagnostic", receive }), "Diagnostic receiving updated")} /></label>
                    <button disabled={busy} onClick={() => void run(() => participation.mutateAsync({ ...p, mode: "off", send: false, receive: false }), "Cloud diagnostics stopped")}>Stop all diagnostic participation</button>
                    <p className="text-slate-500">Receive-only does not authorize replies. Shared limits are managed on the cloud-owned mailbox. Accepted provider sends cannot be recalled.</p>
                </div>}
                {!p && <p className="mt-2 text-xs text-slate-500">Cloud participation controls require an updated Cloud. Legacy state is not proof of diagnostic consent.</p>}
                {cloud?.warmup?.partner_limit && !sendFailure && (
                    <p className="mt-2 pl-10 text-[11.5px] text-slate-500 leading-relaxed">
                        {cloud.warmup.partner_limit.reachable === 0
                            ? "No partner in the pool can take this mailbox's warmup right now, so nothing goes out until one frees up. The cloud checks again every few hours."
                            : `The ramp is at ${cloud.warmup.partner_limit.ramp_target} today, but only ${cloud.warmup.partner_limit.reachable} ${cloud.warmup.partner_limit.reachable === 1 ? "partner is" : "partners are"} available, and a mailbox never writes to the same partner twice in a day.`}
                    </p>
                )}
                {cloud?.health && <WarmupPartnerDiversity health={cloud.health} className="pl-10" />}
            </div>
        </div>
    );
}
