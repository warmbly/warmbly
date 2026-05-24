import type Template from "@/lib/api/models/app/templates/Template";
import Request from "../../Request";

export default async function duplicateTemplate(id: string): Promise<Template> {
    return await Request<Template>({
        method: "POST",
        url: `/templates/${id}/duplicate`,
        authorization: true,
    });
}
