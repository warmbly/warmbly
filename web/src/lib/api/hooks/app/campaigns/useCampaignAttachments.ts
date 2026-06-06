import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
    listAttachments,
    uploadAttachment,
    deleteAttachment,
} from "@/lib/api/client/app/campaigns/attachments";
import type { UploadAttachmentOptions } from "@/lib/api/models/app/campaigns/Attachment";

const key = (campaignId: string) => ["campaigns", campaignId, "attachments"];

export function useCampaignAttachments(campaignId: string) {
    return useQuery({
        queryKey: key(campaignId),
        queryFn: () => listAttachments(campaignId),
        enabled: !!campaignId,
    });
}

export function useUploadAttachment(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ file, opts }: { file: File; opts?: UploadAttachmentOptions }) =>
            uploadAttachment(campaignId, file, opts),
        onSuccess: () => qc.invalidateQueries({ queryKey: key(campaignId) }),
    });
}

export function useDeleteAttachment(campaignId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (attachmentId: string) => deleteAttachment(campaignId, attachmentId),
        onSuccess: () => qc.invalidateQueries({ queryKey: key(campaignId) }),
    });
}
