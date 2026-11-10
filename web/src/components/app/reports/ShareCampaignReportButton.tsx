// Opens the client report scoped to exactly this campaign, creating it the first time.

import React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { FileBarChartIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { usePermission, useWriteGuard } from "@/hooks/usePermission";
import { reportsListQuery, useCreateReport } from "@/lib/api/hooks/app/reports";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { nameError, normalizeName, WORKSPACE_NAME_MAX } from "@/lib/displayName";

function scopedTo(r: ClientReport, campaignId: string): boolean {
    const a = r.access;
    return (
        a?.scope === "restricted" &&
        (a.campaign_ids ?? []).length === 1 &&
        a.campaign_ids[0] === campaignId &&
        (a.campaign_folder_ids ?? []).length === 0 &&
        (a.email_account_ids ?? []).length === 0
    );
}

/** The campaign's name as a report title, or a neutral one when the name would be refused. */
function reportName(campaignName: string): string {
    const name = [...normalizeName(campaignName)].slice(0, WORKSPACE_NAME_MAX).join("").trim();
    return name && !nameError("Name", name, "workspace") ? name : "Campaign report";
}

export default function ShareCampaignReportButton({ campaignId, campaignName }: { campaignId: string; campaignName: string }) {
    const canRead = usePermission("VIEW_ANALYTICS");
    const write = useWriteGuard("MANAGE_CAMPAIGNS");
    const qc = useQueryClient();
    const navigate = useNavigate();
    const create = useCreateReport();
    const [busy, setBusy] = React.useState(false);

    if (!canRead) return null;

    async function open() {
        setBusy(true);
        try {
            // Reports cap at a few hundred per workspace, so every page is read before deciding to create.
            const data = await qc.fetchInfiniteQuery({ ...reportsListQuery, pages: 3 });
            const existing = data.pages.flatMap((p) => p.data ?? []).find((r) => scopedTo(r, campaignId));
            if (existing) {
                await navigate({ to: "/app/reports/$id", params: { id: existing.id } });
                return;
            }
            if (!write.allowed) {
                write.guard()({});
                return;
            }
            const created = await create.mutateAsync({
                name: reportName(campaignName),
                access: { scope: "restricted", campaign_ids: [campaignId], campaign_folder_ids: [], email_account_ids: [] },
            });
            toast.success("Report created");
            await navigate({ to: "/app/reports/$id", params: { id: created.id }, search: { tab: "share" } });
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setBusy(false);
        }
    }

    return (
        <button
            type="button"
            onClick={() => void open()}
            disabled={busy}
            aria-label="Share report"
            title="Share these results with a client"
            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
        >
            {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <FileBarChartIcon className="w-3.5 h-3.5" />}
            <span className="hidden sm:inline">Share report</span>
        </button>
    );
}
