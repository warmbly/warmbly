import Request from "../../Request";

// The opt-in code Warmbly appends to every warmup email a mailbox receives, so
// its owner can filter warmup out in Gmail, Outlook or another inbox tool.
export interface WarmupFilterTag {
    tag: string | null;
}

export async function getWarmupFilterTag(id: string): Promise<WarmupFilterTag> {
    return await Request<WarmupFilterTag>({ method: "GET", url: `/emails/${id}/warmup-filter-tag`, authorization: true });
}

// Without a tag the server issues a new random one.
export async function setWarmupFilterTag(id: string, tag?: string): Promise<WarmupFilterTag> {
    return await Request<WarmupFilterTag>({
        method: "PUT",
        url: `/emails/${id}/warmup-filter-tag`,
        authorization: true,
        data: tag ? { tag } : {},
    });
}

export async function clearWarmupFilterTag(id: string): Promise<WarmupFilterTag> {
    return await Request<WarmupFilterTag>({ method: "DELETE", url: `/emails/${id}/warmup-filter-tag`, authorization: true });
}
