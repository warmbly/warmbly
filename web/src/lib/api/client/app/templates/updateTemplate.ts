import type Template from "@/lib/api/models/app/templates/Template";
import type { UpdateTemplateInput } from "@/lib/api/models/app/templates/Template";
import Request from "../../Request";

export default async function updateTemplate(id: string, data: UpdateTemplateInput): Promise<Template> {
    return await Request<Template>({
        method: "PATCH",
        url: `/templates/${id}`,
        data,
        authorization: true,
    });
}
