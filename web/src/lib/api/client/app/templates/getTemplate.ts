import type Template from "@/lib/api/models/app/templates/Template";
import Request from "../../Request";

export default async function getTemplate(id: string): Promise<Template> {
    return await Request<Template>({
        method: "GET",
        url: `/templates/${id}`,
        authorization: true,
    })
}
