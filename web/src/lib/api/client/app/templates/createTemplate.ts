import type Template from "@/lib/api/models/app/templates/Template";
import Request from "../../Request";

export default async function createTemplate(data: { name: string; subject: string; body: string; variables?: string[] }): Promise<Template> {
    return await Request<Template>({
        method: "POST",
        url: `/templates`,
        data,
        authorization: true,
    })
}
