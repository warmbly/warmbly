// Settings → Warmbly Cloud.
//
// Self-hosted: link this instance to the hosted warmup pool and pick the
// mailboxes it warms (GET/POST /cloud-link/*). Cloud: the self-hosted
// instances linked to this workspace (GET /pool-link/instances).

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import { CheckIcon, CloudIcon, ExternalLinkIcon, Loader2Icon, RefreshCwIcon, SparklesIcon } from "lucide-react";
import { useInstanceAdmin, usePermission } from "@/hooks/usePermission";
import { useConfirm } from "@/hooks/context/confirm";
import { NoAccess } from "@/components/layout/NoAccess";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useCloudLinkStatus, useDisconnectCloudLink } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import type { PoolLinkPlan } from "@/lib/api/models/app/cloudlink/CloudLink";
import { WARMUP_PLAN_BENEFITS, WARMUP_PLAN_PITCH } from "@/lib/plans";
import { Row, Section, SectionShell } from "../_components/SectionShell";
import ConnectFlow from "./ConnectFlow";
import MailboxTable from "./MailboxTable";
import LinkedInstances from "./LinkedInstances";

export default function WarmblyCloudSettingsPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    const authConfig = useAuthConfig();
    const organization = useCurrentOrganization();
    const instanceAdmin = useInstanceAdmin();
    if (!canManage) return <NoAccess feature="Warmbly Cloud" permissionLabel="Manage settings" />;
    if (authConfig.data && !authConfig.data.self_hosted) {
        return (
            <SectionShell title="Linked instances" description="Self-hosted Warmbly instances that warm their mailboxes in this workspace's pool.">
                <Section eyebrow="Instances" description="Each instance enrolls its own mailboxes. Unlinking removes them from the pool.">
                    <LinkedInstances />
                </Section>
            </SectionShell>
        );
    }
    if (!instanceAdmin.allowed) {
        return (
            <SectionShell title="Warmbly Cloud" description="Warm your mailboxes in the Warmbly pool while everything else stays on this server.">
                <Section eyebrow="Connection">
                    <p className="text-[12.5px] text-slate-500 leading-relaxed">
                        {instanceAdmin.holdsAdmin
                            ? "Managing a workspace's Cloud connection needs a session with two-factor authentication. Turn on 2FA or add a passkey under Settings > Security, then sign in again."
                            : "An administrator of this instance manages this workspace's Cloud connection."}
                    </p>
                </Section>
            </SectionShell>
        );
    }
    return <SelfHostedCloud key={organization.data?.id} />;
}

function SelfHostedCloud() {
    const status = useCloudLinkStatus();
    const disconnect = useDisconnectCloudLink();
    const confirm = useConfirm();
    // The step flow stays up after linking until the user finishes it, so a
    // fresh link walks through mailbox selection instead of dropping into the
    // steady-state table.
    const [flow, setFlow] = React.useState<boolean | null>(null);
    const showFlow = flow ?? (status.data ? !status.data.connected : false);

    if (status.isLoading || !status.data) {
        return (
            <SectionShell title="Warmbly Cloud" description="Warm your mailboxes in the Warmbly pool while everything else stays on this server.">
                <div className="py-10 flex justify-center text-slate-400">
                    <Loader2Icon className="w-4 h-4 animate-spin" />
                </div>
            </SectionShell>
        );
    }
    const st = status.data;
    const plan = st.info?.plan;
    const legacy = st.connected && !st.link?.organization_id;

    return (
        <SectionShell
            title="Warmbly Cloud"
            description="Warm your mailboxes in the Warmbly pool while everything else stays on this server."
            actions={
                st.connected ? (
                    <button
                        type="button"
                        onClick={() => void status.refetch()}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5"
                    >
                        <RefreshCwIcon className={`w-3 h-3 ${status.isFetching ? "animate-spin" : ""}`} />
                        Refresh
                    </button>
                ) : undefined
            }
        >
            {legacy && (
                <Section eyebrow="Legacy connection" description="Existing enrolled mailboxes keep working on the old instance-wide connection. New mailboxes need a separate connection for this workspace, using a Cloud workspace with no active link. The old Cloud workspace stays occupied until the legacy link is disconnected.">
                    <button type="button" onClick={() => setFlow(true)} className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium">
                        Connect this workspace
                    </button>
                </Section>
            )}
            {st.legacy_connected && !legacy && (
                <Section eyebrow="Legacy instance link" description="Some existing mailboxes may still use the old instance-wide connection. Keep it until those mailboxes have moved. Disconnecting it affects all local workspaces, not this workspace's new connection.">
                    <button type="button" className="h-7 px-2.5 rounded-md text-[12px] text-rose-600 hover:bg-rose-50" onClick={() => confirm.show("Disconnect the legacy instance link across all workspaces? Existing mailboxes still using it will stop using Cloud, and its managed mailbox mirrors will be removed.", async () => {
                        try {
                            await disconnect.mutateAsync(true);
                            toast.success("Legacy link disconnected");
                        } catch (e) {
                            toast.error(buildError(e as AppError));
                        }
                    })}>Disconnect legacy link</button>
                </Section>
            )}
            <AnimatePresence mode="wait" initial={false}>
                {showFlow ? (
                    <motion.div key="flow" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
                        <ConnectFlow
                            status={legacy ? { ...st, connected: false } : st}
                            onFinished={() => {
                                setFlow(false);
                                void status.refetch();
                            }}
                        />
                    </motion.div>
                ) : (
                    <motion.div key="steady" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="divide-y divide-slate-200/70">
                        <Section eyebrow="Connection">
                            <Row
                                label={
                                    <span className="inline-flex items-center gap-2">
                                        <span className="size-6 rounded-md bg-sky-600 text-white inline-flex items-center justify-center">
                                            <CloudIcon className="w-3.5 h-3.5" />
                                        </span>
                                        {st.link?.organization_name || "Warmbly Cloud"}
                                    </span>
                                }
                                description={
                                    st.reachable ? (
                                        <span>Connected{st.link?.connected_at ? ` since ${new Date(st.link.connected_at).toLocaleDateString()}` : ""}</span>
                                    ) : (
                                        <span className="text-amber-700">Cloud unreachable{st.error ? `: ${st.error}` : ""}</span>
                                    )
                                }
                            />
                        </Section>
                        {plan && (
                            <Section eyebrow="Plan" description="Billed on Warmbly Cloud. Each connected local workspace needs its own Cloud workspace and plan.">
                                <PlanCard plan={plan} />
                            </Section>
                        )}
                        <Section
                            eyebrow="Mailboxes"
                            description="Enrolled mailboxes are warmed by Warmbly Cloud; their local warmup stops. Campaigns keep sending from this server."
                        >
                            <MailboxTable allowEnrollment={!legacy} />
                        </Section>
                        <Section eyebrow="Disconnect">
                            <Row
                                danger
                                label={legacy ? "Disconnect legacy instance link" : "Disconnect this workspace from Warmbly Cloud"}
                                description={legacy ? "Stops all mailboxes still using this legacy link across every local workspace. Workspace-scoped connections are not affected." : "Stops only mailboxes and redirects using this connection. Older legacy enrollments and other workspaces are not affected. Managed mailbox mirrors using this link are removed."}
                            >
                                <button
                                    type="button"
                                    onClick={() =>
                                        confirm.show(legacy ? "Disconnect the legacy instance link? All mailboxes still using it, across all workspaces, will stop warming on Cloud." : "Disconnect this workspace from Warmbly Cloud? Mailboxes and redirects using this connection will stop using Cloud.", async () => {
                                            try {
                                                await disconnect.mutateAsync(legacy);
                                                setFlow(null);
                                                toast.success("Disconnected");
                                            } catch (e) {
                                                toast.error(buildError(e as AppError));
                                            }
                                        })
                                    }
                                    className="h-7 px-2.5 rounded-md text-[12px] text-rose-600 hover:bg-rose-50 transition-colors"
                                >
                                    Disconnect
                                </button>
                            </Row>
                        </Section>
                    </motion.div>
                )}
            </AnimatePresence>
        </SectionShell>
    );
}

// The pool plan as the cloud reports it. Free shows what is left of the
// allowance and what Premium adds; Premium shows what it holds. Both act on
// the cloud's own billing page, since the instance bills nothing itself.
function PlanCard({ plan }: { plan: PoolLinkPlan }) {
    const premium = plan.tier === "paid";
    const limit = plan.mailbox_limit;
    const atLimit = !premium && limit !== null && plan.enrolled >= limit;
    return (
        <div className={`rounded-lg border overflow-hidden ${premium ? "border-sky-200/70" : "border-slate-200"}`}>
            <div className={`px-4 py-3.5 flex flex-wrap items-start gap-3 ${premium ? "bg-gradient-to-b from-sky-50/70 to-white" : "bg-gradient-to-b from-slate-50/60 to-white"}`}>
                <span className={`size-8 rounded-md inline-flex items-center justify-center shrink-0 ${premium ? "bg-sky-600 text-white" : "bg-slate-100 text-slate-600"}`}>
                    <SparklesIcon className="w-4 h-4" />
                </span>
                <div className="min-w-0 flex-1 basis-[220px]">
                    <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-[14px] font-semibold text-slate-900">{premium ? "Premium" : "Free"}</span>
                        <span className={`inline-flex items-center h-4 px-1.5 rounded text-[10px] uppercase tracking-[0.1em] font-medium border ${
                            premium ? "bg-sky-50 text-sky-700 border-sky-100" : atLimit ? "bg-amber-50 text-amber-700 border-amber-100" : "bg-slate-100 text-slate-500 border-slate-200"
                        }`}>
                            {premium ? "Unlimited" : limit === null ? "Unlimited" : `${plan.enrolled} of ${limit} mailboxes`}
                        </span>
                    </div>
                    <p className="text-[12px] text-slate-500 mt-1 leading-relaxed max-w-md">
                        {premium
                            ? "Your mailboxes warm in the premium pool, built for inbox placement, with reputation protection and priority matching."
                            : atLimit
                              ? `Every free pool mailbox is in use. ${WARMUP_PLAN_PITCH}`
                              : `Up to ${limit ?? 10} mailboxes in the free pool. ${WARMUP_PLAN_PITCH}`}
                    </p>
                    {/* Labelled, so a free workspace does not read these as already included. */}
                    {!premium && (
                        <div className="mt-2.5">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Premium includes</div>
                            <ul className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-slate-700">
                                {WARMUP_PLAN_BENEFITS.map((b) => (
                                    <li key={b.title} className="inline-flex items-center gap-1.5" title={b.body}>
                                        <CheckIcon className="w-3 h-3 text-sky-600" />
                                        {b.title}
                                    </li>
                                ))}
                            </ul>
                        </div>
                    )}
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    {!premium && plan.upgrade_url && (
                        <a
                            href={plan.upgrade_url}
                            target="_blank"
                            rel="noreferrer"
                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                        >
                            Premium for ${plan.price_usd}/mo
                            <ExternalLinkIcon className="w-3 h-3" />
                        </a>
                    )}
                    {premium && plan.manage_url && (
                        <a
                            href={plan.manage_url}
                            target="_blank"
                            rel="noreferrer"
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                        >
                            Manage on Warmbly Cloud
                            <ExternalLinkIcon className="w-3 h-3" />
                        </a>
                    )}
                </div>
            </div>
        </div>
    );
}
