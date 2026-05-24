import type { RenderedTemplate } from "@/lib/api/models/app/templates/Template";
import Request from "../../Request";

export default async function renderTemplate(
    id: string,
    variables: Record<string, string>,
): Promise<RenderedTemplate> {
    return await Request<RenderedTemplate>({
        method: "POST",
        url: `/templates/${id}/render`,
        data: { variables },
        authorization: true,
    });
}
