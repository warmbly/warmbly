import Request from "../../Request";

// Removes an event→action subscription from a connection.
export default async function deleteConnectionEvent(input: {
    connectionId: string;
    eventId: string;
}): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/integrations/connections/${input.connectionId}/events/${input.eventId}`,
        authorization: true,
    });
}
