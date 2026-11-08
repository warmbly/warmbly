import Request from "../../Request";

// Takes the current workspace out of its agency. Owner only.
export default async function leaveAgency(): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/organization/agency/leave`,
        authorization: true,
    });
}
