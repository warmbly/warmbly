import { useState } from "react";
import { Link, Outlet, useLocation, useParams } from "react-router-dom";
import { motion } from "framer-motion";
import {
    BarChart3Icon,
    CalendarIcon,
    ListChecksIcon,
    Loader2Icon,
    PauseIcon,
    PlayIcon,
    Settings2Icon,
    UsersIcon,
} from "lucide-react";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import useStartCampaign from "@/lib/api/hooks/app/campaigns/useStartCampaign";
import useStopCampaign from "@/lib/api/hooks/app/campaigns/useStopCampaign";
import { CampaignContext } from "@/hooks/context/campaign";
import { useConfirm } from "@/hooks/context/confirm";
import LaunchCampaignDialog from "@/components/app/campaigns/LaunchCampaignDialog";

const TABS = [
    { label: "Overview", path: "", Icon: BarChart3Icon },
    { label: "Leads", path: "/leads", Icon: UsersIcon },
    { label: "Steps", path: "/sequences", Icon: ListChecksIcon },
    { label: "Schedule", path: "/schedule", Icon: CalendarIcon },
    { label: "Settings", path: "/preferences", Icon: Settings2Icon },
] as const;

const STATUS_PILL: Record<string, string> = {
    active: "bg-emerald-50 text-emerald-700 border-emerald-200",
    paused: "bg-amber-50 text-amber-700 border-amber-200",
    draft: "bg-slate-100 text-slate-600 border-slate-200",
    completed: "bg-slate-100 text-slate-600 border-slate-200",
};

export default function CampaignLayout() {
    const { pathname } = useLocation();
    const { id } = useParams();
    const campaignData = useCampaign(id ?? "");
    const confirm = useConfirm();
    const startCampaign = useStartCampaign();
    const stopCampaign = useStopCampaign();
    const [launchOpen, setLaunchOpen] = useState(false);

    if (campaignData.isLoading) {
        return (
            <div className="px-5 pt-5 space-y-4">
                <div className="space-y-2">
                    <div className="h-6 w-56 bg-slate-100 rounded-md animate-pulse" />
                    <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                </div>
                <div className="flex gap-2">
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
    const pill = STATUS_PILL[status] ?? STATUS_PILL.draft;

    const isActive = status === "active";
    const canStart = status === "draft" || status === "paused";
    const canToggle = isActive || canStart;
    const pending = isActive ? stopCampaign.isPending : startCampaign.isPending;

    const onToggle = () => {
        if (isActive) {
            confirm?.show(`Pause ${campaign.name}?`, () => {
                stopCampaign.mutate(campaign.id);
            });
        } else {
            setLaunchOpen(true);
        }
    };

    return (
        <CampaignContext.Provider value={campaign}>
            <div className="flex flex-col min-h-full bg-white">
                <div className="px-5 pt-5 pb-3 flex items-start gap-3">
                    <div className="min-w-0">
                        <div className="flex items-center gap-2">
                            <h1 className="text-[18px] font-semibold text-slate-900 truncate">{campaign.name}</h1>
                            <span
                                className={`shrink-0 inline-flex items-center h-5 px-2 rounded-md border text-[10px] uppercase tracking-[0.12em] font-medium ${pill}`}
                            >
                                {status}
                            </span>
                        </div>
                        <p className="text-[11px] text-slate-400 font-mono mt-1 truncate">{campaign.id}</p>
                    </div>

                    {canToggle && (
                        <div className="ml-auto shrink-0">
                            <button
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
                            </button>
                        </div>
                    )}
                </div>

                <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 overflow-x-auto no-scrollbar">
                    {TABS.map(({ label, path, Icon }) => {
                        const fullPath = `/app/campaigns/${id}${path}`;
                        const isTabActive = pathname.replace(/\/$/, "") === fullPath.replace(/\/$/, "");
                        return (
                            <Link
                                key={path || "overview"}
                                to={fullPath}
                                className={`relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] transition-colors ${
                                    isTabActive
                                        ? "text-slate-900 font-medium"
                                        : "text-slate-500 hover:text-slate-800"
                                }`}
                            >
                                <Icon className="w-3.5 h-3.5" />
                                {label}
                                {isTabActive && (
                                    <motion.span
                                        layoutId="campaign-tab-underline"
                                        className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                        transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                    />
                                )}
                            </Link>
                        );
                    })}
                </div>

                <div className="p-5">
                    <Outlet />
                </div>
            </div>
            <LaunchCampaignDialog
                campaign={launchOpen ? campaign : null}
                onClose={() => setLaunchOpen(false)}
                onConfirm={(cid) => startCampaign.mutateAsync(cid)}
            />
        </CampaignContext.Provider>
    );
}
