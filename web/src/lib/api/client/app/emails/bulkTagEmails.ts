import Request from "../../Request";

export default async function bulkTagEmails(
    emailIds: string[],
    addTags: string[],
    removeTags: string[],
): Promise<{ updated: number }> {
    return await Request<{ updated: number }>({
        method: "PATCH",
        url: `/emails/tags`,
        data: { email_ids: emailIds, add_tags: addTags, remove_tags: removeTags },
        authorization: true,
    })
}
