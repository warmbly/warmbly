import Request from "../../Request";

export interface NodePosition {
    id: string;
    x: number;
    y: number;
}

// Position-only persist so a teammate's arrangement sticks across visits. Written
// continuously as cards are dragged; cheap and unaudited on the server.
export default async function updateAutomationLayout(id: string, positions: NodePosition[]): Promise<{ ok: boolean }> {
    return await Request<{ ok: boolean }>({
        method: "PATCH",
        url: `/automations/${id}/layout`,
        data: { positions },
        authorization: true,
    });
}
