// Warmup copy: the workspace's own copy that its warmup pool carries (running
// campaign steps and warmup templates), and where each piece landed. Live
// through the warmup_template, campaign and step audit spine; nothing polls.

import React from "react";
import { PlusIcon } from "lucide-react";
import { Page, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import { Segmented } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { usePermission } from "@/hooks/usePermission";
import { useSearchParams } from "@/hooks/useSearchParams";
import { useWarmupCopySettings } from "@/lib/api/hooks/app/warmup/useWarmupCopy";
import {
    DEFAULT_WARMUP_COPY_DAYS,
    WARMUP_COPY_DAYS,
    parseWarmupCopyDays,
    type WarmupCopyDays,
} from "@/lib/api/models/app/warmup/WarmupCopy";
import WarmupCopySettingsCard from "@/components/app/warmup-copy/WarmupCopySettingsCard";
import CampaignCopySection from "@/components/app/warmup-copy/CampaignCopySection";
import TemplatesSection from "@/components/app/warmup-copy/TemplatesSection";
import NewWarmupTemplateDialog from "@/components/app/warmup-copy/NewWarmupTemplateDialog";
import { CopyPlacementLegend, HEADLINE_NOTE } from "@/components/app/warmup-copy/copyParts";

const DAY_OPTIONS = WARMUP_COPY_DAYS.map((d) => ({ value: String(d) as `${WarmupCopyDays}`, label: `${d} days` }));

export default function WarmupCopyPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    if (!canManage) return <NoAccess feature="warmup copy" permissionLabel="Manage settings" />;
    return <WarmupCopy />;
}

function WarmupCopy() {
    const [params, setParams] = useSearchParams();
    const days = parseWarmupCopyDays(params.get("days"));
    const settings = useWarmupCopySettings();
    // Campaign copy names campaigns and their subjects, so it follows campaign access.
    const canViewCampaigns = usePermission("VIEW_CAMPAIGNS");
    const [creating, setCreating] = React.useState(false);

    const setDays = (d: string) => {
        const next = new URLSearchParams(params);
        if (Number(d) === DEFAULT_WARMUP_COPY_DAYS) next.delete("days");
        else next.set("days", d);
        setParams(next, { replace: true });
    };

    return (
        <Page>
            <PageTopbar eyebrow="Warmup copy" subtitle="Your own copy in warmup">
                <TopbarAction icon={<PlusIcon className="w-3.5 h-3.5" />} onClick={() => setCreating(true)}>
                    New template
                </TopbarAction>
            </PageTopbar>

            <div className="px-5 py-3 border-b border-slate-200">
                <p className="text-[12.5px] text-slate-600 leading-relaxed max-w-[72ch]">
                    Warmup can carry your own copy, the opening steps of running campaigns and templates you add, so you see
                    where it lands before your prospects get it.
                </p>
            </div>

            <WarmupCopySettingsCard />

            <div className="px-5 min-h-10 py-1.5 border-b border-slate-200 flex flex-wrap items-center gap-x-3 gap-y-1.5">
                <Segmented value={String(days) as `${WarmupCopyDays}`} onChange={setDays} options={DAY_OPTIONS} />
                <CopyPlacementLegend className="ml-auto" />
            </div>

            {canViewCampaigns ? (
                <CampaignCopySection days={days} enabled={settings.data?.campaign_copy ?? true} />
            ) : (
                <p className="px-5 py-3 border-b border-slate-200 text-[11.5px] text-slate-500">
                    Running campaign steps are listed for members who can view campaigns.
                </p>
            )}
            <TemplatesSection days={days} onNew={() => setCreating(true)} />

            <p className="px-5 py-4 text-[11px] text-slate-400 leading-relaxed max-w-[80ch]">{HEADLINE_NOTE}</p>

            <NewWarmupTemplateDialog open={creating} onClose={() => setCreating(false)} />
        </Page>
    );
}
