import type Template from "@/lib/api/models/app/templates/Template";
import type { CreateTemplateInput } from "@/lib/api/models/app/templates/Template";
import Request from "../../Request";

export default async function createTemplate(data: CreateTemplateInput): Promise<Template> {
    return await Request<Template>({
        method: "POST",
        url: `/templates`,
        data,
        authorization: true,
    });
}
