import { useMutation, useQuery } from "@tanstack/react-query";
import {
    copyCampaignToWorkspaces,
    copyTemplatesToWorkspaces,
    listAgencyGroups,
} from "@/lib/api/client/app/workspaceCopy/copyToWorkspaces";

// Copies land in other workspaces, so nothing in this workspace's cache changes;
// those workspaces refresh through their own audit events.
export function useCopyCampaignToWorkspaces() {
    return useMutation({
        mutationFn: (v: { campaignId: string; organizationIds: string[]; templateIds: string[]; key: string }) =>
            copyCampaignToWorkspaces(v.campaignId, v.organizationIds, v.templateIds, v.key),
    });
}

export function useCopyTemplatesToWorkspaces() {
    return useMutation({
        mutationFn: (v: { templateIds: string[]; organizationIds: string[]; key: string }) =>
            copyTemplatesToWorkspaces(v.templateIds, v.organizationIds, v.key),
    });
}

// Agency grouping for the destination list. An instance or account without
// agencies answers an empty list, and a failure falls back to a plain list.
export function useAgencyGroups(enabled: boolean) {
    return useQuery({
        queryKey: ["workspace-copy", "agency-groups"],
        queryFn: listAgencyGroups,
        enabled,
        staleTime: 60_000,
        retry: false,
    });
}
