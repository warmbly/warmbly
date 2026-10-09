import { useEffect, useState } from "react";
import PermissionButton from "@/components/ui/PermissionButton";
import { Link, Outlet, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { useSearchParams } from "@/hooks/useSearchParams";
import { motion } from "framer-motion";
import {
    ArrowLeftIcon,
    BarChart3Icon,
    CalendarIcon,
    ListChecksIcon,
    Loader2Icon,
    PauseIcon,
    PlayIcon,
    Settings2Icon,
    UsersIcon,
} from "lucide-react";
import { campaignDisplayLabel, isIdleCampaign } from "@/components/app/campaigns/status";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import useStartCampaign from "@/lib/api/hooks/app/campaigns/useStartCampaign";
import useStopCampaign from "@/lib/api/hooks/app/campaigns/useStopCampaign";
import { CampaignContext } from "@/hooks/context/campaign";
import { useConfirm } from "@/hooks/context/confirm";
import LaunchCampaignDialog from "@/components/app/campaigns/LaunchCampaignDialog";
import CampaignActionsMenu from "@/components/app/campaigns/CampaignActionsMenu";
import UndeliverableBanner from "@/components/app/campaigns/UndeliverableBanner";
import { canStartCampaign } from "@/components/app/campaigns/useCampaignActions";
import toast from "react-hot-toast/headless";
import { CAMPAIGN_DELETED_EVENT, type CampaignDeletedDetail } from "@/lib/realtime/campaignDeleted";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import { useAccessRestricted, usePermission } from "@/hooks/usePermission";

// `scoped` tabs read only the campaign's own data, so a restricted member sees them.
const TABS = [
    { label: "Overview", to: "/app/campaigns/$id", Icon: BarChart3Icon, scoped: true },
    { label: "Leads", to: "/app/campaigns/$id/leads", Icon: UsersIcon, scoped: false },
    { label: "Steps", to: "/app/campaigns/$id/steps", Icon: ListChecksIcon, scoped: true },
    { label: "Schedule", to: "/app/campaigns/$id/schedule", Icon: CalendarIcon, scoped: true },
    { label: "Settings", to: "/app/campaigns/$id/preferences", Icon: Settings2Icon, scoped: false },
] as const;

const STATUS_PILL: Record<string, string> = {
    active: "bg-emerald-50 text-emerald-700 border-emerald-200",
    paused: "bg-amber-50 text-amber-700 border-amber-200",
    paused_undeliverable: "bg-amber-50 text-amber-700 border-amber-200",
    draft: "bg-slate-100 text-slate-600 border-slate-200",
    completed: "bg-slate-100 text-slate-600 border-slate-200",
    idle: "bg-sky-50 text-sky-700 border-sky-200",
};

export default function CampaignLayout() {
    const pathname = useLocation({ select: (l) => l.pathname });
    const { id } = useParams({ from: "/app/campaigns/$id" });
    const navigate = useNavigate();
    const campaignData = useCampaign(id);
    const confirm = useConfirm();
    const startCampaign = useStartCampaign();
    const stopCampaign = useStopCampaign();
    const [launchOpen, setLaunchOpen] = useState(false);
    const [searchParams, setSearchParams] = useSearchParams();
    const canSend = usePermission("SEND_CAMPAIGNS");
    const restricted = useAccessRestricted();
    const launchRequested = searchParams.get("launch") === "1";
    const loadedStatus = campaignData.data?.status;

    // ?launch=1 (the new-campaign flow's hand-off, the draft checklist) opens
    // the launch dialog once the campaign has loaded, then leaves the URL clean.
    useEffect(() => {
        if (!launchRequested || !loadedStatus) return;
        if (canSend && canStartCampaign(loadedStatus)) setLaunchOpen(true);
        setSearchParams(
            (prev) => {
                const next = new URLSearchParams(prev);
                next.delete("launch");
                return next;
            },
            { replace: true },
        );
    }, [launchRequested, loadedStatus, canSend, setSearchParams]);

    // Collaboration: claim this campaign while it's open so teammates see
    // who's already in here (header pill + the org-wide presence stack).
    usePresenceResource(id ? `campaign:${id}` : null);

    // A teammate deleted the campaign this page shows: leave it with a note
    // instead of refetching into a 404.
    useEffect(() => {
        if (!id) return;
        const onDeleted = (e: Event) => {
            const detail = (e as CustomEvent<CampaignDeletedDetail>).detail;
            if (detail?.id !== id) return;
            toast(`"${detail.name || "This campaign"}" was deleted by a teammate`);
            navigate({ to: "/app/campaigns", replace: true });
        };
        window.addEventListener(CAMPAIGN_DELETED_EVENT, onDeleted);
        return () => window.removeEventListener(CAMPAIGN_DELETED_EVENT, onDeleted);
    }, [id, navigate]);

    if (campaignData.isLoading) {
        return (
            <div className="px-3 sm:px-5 pt-4 sm:pt-5 space-y-4">
                <div className="space-y-2">
                    <div className="h-6 w-56 bg-slate-100 rounded-md animate-pulse" />
                    <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                </div>
                <div className="flex gap-2 overflow-hidden">
                    {[...Array(5)].map((_, i) => (
                        <div key={i} className="h-8 w-20 bg-slate-100 rounded-md animate-pulse" />
                    ))}
                </div>
            </div>
        );
    }

    if (campaignData.isError || !campaignData.data) {
        return (
            <div className="flex flex-col items-center justify-center py-24 text-center">
                <p className="text-[13px] font-medium text-slate-900">Couldn't load this campaign</p>
                <p className="text-[12px] text-slate-400 mt-1 max-w-[34ch]">
                    It may have been deleted, or you don't have access in this workspace.
                </p>
                <Link
                    to="/app/campaigns"
                    className="mt-4 inline-flex items-center h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
                >
                    Back to campaigns
                </Link>
            </div>
        );
    }

    const campaign = campaignData.data;
    const status = campaign.status;
    const pill = isIdleCampaign(campaign) ? STATUS_PILL.idle : (STATUS_PILL[status] ?? STATUS_PILL.draft);

    const isActive = status === "active";
    const canStart = canStartCampaign(status);
    const canToggle = isActive || canStart;
    const pending = isActive ? stopCampaign.isPending : startCampaign.isPending;

    const onToggle = () => {
        if (isActive) {
            confirm?.show(`Pause ${campaign.name}?`, () => {
                stopCampaign.mutate(campaign.id, {
                    onError: (err) => toast.error((err as { message?: string })?.message || "Could not pause the campaign"),
                });
            });
        } else {
            setLaunchOpen(true);
        }
    };

    return (
        <CampaignContext.Provider value={campaign}>
            <div className="flex flex-col min-h-full bg-white">
                <div className="px-3 sm:px-5 pt-3 sm:pt-4 pb-3 flex items-start gap-3">
                    <div className="min-w-0">
                        <Link
                            to="/app/campaigns"
                            className="inline-flex items-center gap-1 h-6 -ml-1.5 px-1.5 mb-1 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            <ArrowLeftIcon className="w-3 h-3" />
                            Campaigns
                        </Link>
                        {/* min-w-0 lets the name truncate instead of pushing the
                            pills off a narrow screen; the pills wrap under it. */}
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                            <h1 className="min-w-0 max-w-full text-[18px] font-semibold text-slate-900 truncate">{campaign.name}</h1>
                            <span
                                className={`shrink-0 inline-flex items-center h-5 px-2 rounded-md border text-[10px] uppercase tracking-[0.12em] font-medium ${pill}`}
                            >
                                {campaignDisplayLabel(campaign)}
                            </span>
                            <ResourceViewers resource={`campaign:${campaign.id}`} className="shrink-0" />
                        </div>
                        <p className="text-[11px] text-slate-400 font-mono mt-1 truncate">{campaign.id}</p>
                    </div>

                    <div className="ml-auto shrink-0 flex items-center gap-1.5">
                        {canToggle && (
                            <PermissionButton
                                permission="SEND_CAMPAIGNS"
                                type="button"
                                onClick={onToggle}
                                disabled={pending}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {pending ? (
                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                ) : isActive ? (
                                    <PauseIcon className="w-3.5 h-3.5" />
                                ) : (
                                    <PlayIcon className="w-3.5 h-3.5" />
                                )}
                                {isActive ? "Pause" : "Start"}
                            </PermissionButton>
                        )}
                        <CampaignActionsMenu
                            campaign={campaign}
                            variant="header"
                            onToggle={canToggle ? onToggle : undefined}
                            afterDelete={() => navigate({ to: "/app/campaigns", replace: true })}
                        />
                    </div>
                </div>

                <UndeliverableBanner campaignId={campaign.id} status={status} />

                <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 overflow-x-auto no-scrollbar">
                    {TABS.filter((t) => t.scoped || !restricted).map(({ label, to, Icon }) => (
                        <Link
                            key={to}
                            to={to}
                            params={{ id }}
                            activeOptions={{ exact: true, includeSearch: false }}
                            className="relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] transition-colors"
                            activeProps={{ className: "text-slate-900 font-medium" }}
                            inactiveProps={{ className: "text-slate-500 hover:text-slate-800" }}
                        >
                            {({ isActive }) => (
                                <>
                                    <Icon className="w-3.5 h-3.5" />
                                    {label}
                                    {isActive && (
                                        <motion.span
                                            layoutId="campaign-tab-underline"
                                            className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                            transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                        />
                                    )}
                                </>
                            )}
                        </Link>
                    ))}
                </div>

                {/* Leads renders a full-bleed Page (its own topbar, stat strip
                    and table gutters), so padding it again wastes a fifth of a
                    phone screen and stops its hairlines short of the edge. */}
                <div className={pathname.replace(/\/$/, "").endsWith("/leads") ? "" : "p-3 sm:p-5"}>
                    <Outlet />
                </div>
            </div>
            <LaunchCampaignDialog
                campaign={launchOpen ? campaign : null}
                onClose={() => setLaunchOpen(false)}
                onConfirm={(cid, options) => startCampaign.mutateAsync({ id: cid, options })}
            />
        </CampaignContext.Provider>
    );
}
