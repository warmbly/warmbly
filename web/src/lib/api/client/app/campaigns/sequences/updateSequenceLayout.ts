import Request from "../../../Request";

export interface SequencePosition {
    id: string;
    x: number;
    y: number;
}

// Position-only persist for the sequence canvas so a teammate's arrangement
// sticks across visits. Cheap and unaudited on the server; only real step ids
// (UUIDs) are persisted, synthetic nodes are ignored.
export default async function updateSequenceLayout(campaign_id: string, positions: SequencePosition[]): Promise<{ ok: boolean }> {
    return await Request<{ ok: boolean }>({
        method: "PATCH",
        url: `/campaigns/${campaign_id}/step-layout`,
        data: { positions },
        authorization: true,
    });
}
