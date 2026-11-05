import type {
    CreatedWarmupTemplate,
    WarmupCampaignCopyList,
    WarmupCopySettings,
    WarmupTemplateCheck,
    WarmupTemplateInput,
    WarmupTemplate,
    WarmupTemplateList,
    WarmupTemplateStats,
} from "@/lib/api/models/app/warmup/WarmupCopy";
import Request from "../../Request";

export async function getWarmupCopySettings(): Promise<WarmupCopySettings> {
    return await Request<WarmupCopySettings>({
        method: "GET",
        url: "/warmup/templates/settings",
        authorization: true,
    });
}

// A full-state write: every field is required, so a retry lands on the same settings.
export async function putWarmupCopySettings(settings: WarmupCopySettings): Promise<WarmupCopySettings> {
    return await Request<WarmupCopySettings>({
        method: "PUT",
        url: "/warmup/templates/settings",
        data: settings,
        authorization: true,
    });
}

export async function listWarmupCampaignCopy(days: number): Promise<WarmupCampaignCopyList> {
    return await Request<WarmupCampaignCopyList>({
        method: "GET",
        url: `/warmup/templates/campaign-copy?days=${days}`,
        authorization: true,
    });
}

export async function listWarmupTemplates(days: number): Promise<WarmupTemplateList> {
    return await Request<WarmupTemplateList>({
        method: "GET",
        url: `/warmup/templates?days=${days}`,
        authorization: true,
    });
}

export async function createWarmupTemplate(body: WarmupTemplateInput): Promise<CreatedWarmupTemplate> {
    return await Request<CreatedWarmupTemplate>({
        method: "POST",
        url: "/warmup/templates",
        data: body,
        authorization: true,
    });
}

// Analyzes a draft and stores nothing.
export async function checkWarmupTemplate(
    body: { subject: string; body: string },
    signal?: AbortSignal,
): Promise<WarmupTemplateCheck> {
    return await Request<WarmupTemplateCheck>({
        method: "POST",
        url: "/warmup/templates/check",
        data: body,
        authorization: true,
        signal,
    });
}

export async function getWarmupTemplateStats(id: string, days: number): Promise<WarmupTemplateStats> {
    return await Request<WarmupTemplateStats>({
        method: "GET",
        url: `/warmup/templates/${id}/stats?days=${days}`,
        authorization: true,
    });
}

export async function deleteWarmupTemplate(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/warmup/templates/${id}`,
        authorization: true,
    });
}

export async function getWarmupTemplate(id: string): Promise<WarmupTemplate> {
    const res = await Request<{ template: WarmupTemplate }>({
        method: "GET",
        url: `/warmup/templates/${id}`,
        authorization: true,
    });
    return res.template;
}
