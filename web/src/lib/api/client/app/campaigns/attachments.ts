import type Attachment from "@/lib/api/models/app/campaigns/Attachment";
import type { UploadAttachmentOptions } from "@/lib/api/models/app/campaigns/Attachment";
import Request from "../../Request";

// Campaign attachments. The upload sidesteps the JSON body shape because the
// payload is multipart/form-data (the file rides the "file" field), mirroring
// avatar/uploadUserAvatar. List/delete are plain JSON requests.

export async function listAttachments(campaignId: string): Promise<Attachment[]> {
    const res = await Request<{ data: Attachment[] }>({
        method: "GET",
        url: `/campaigns/${campaignId}/attachments`,
        authorization: true,
    });
    return res.data ?? [];
}

export async function uploadAttachment(
    campaignId: string,
    file: File,
    opts?: UploadAttachmentOptions,
): Promise<Attachment> {
    const fd = new FormData();
    fd.append("file", file, file.name);
    if (opts?.sequenceId) fd.append("sequence_id", opts.sequenceId);
    return await Request<Attachment>({
        method: "POST",
        url: `/campaigns/${campaignId}/attachments`,
        data: fd,
        authorization: true,
    });
}

export async function deleteAttachment(campaignId: string, attachmentId: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/campaigns/${campaignId}/attachments/${attachmentId}`,
        authorization: true,
    });
}
