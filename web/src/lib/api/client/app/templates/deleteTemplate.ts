import Request from "../../Request";

export default async function deleteTemplate(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/templates/${id}`,
        authorization: true,
    })
}
