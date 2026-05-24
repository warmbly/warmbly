import Request from "../../Request";

export default async function deleteCategory(id: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/categories/${id}`,
        authorization: true,
    })
}
