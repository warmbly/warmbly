import type Template from "@/lib/api/models/app/templates/Template";
import type { TemplatesResult } from "@/lib/api/models/app/templates/Template";
import Request from "../../Request";

export default async function reorderTemplates(ids: string[]): Promise<Template[]> {
    const res = await Request<TemplatesResult>({
        method: "PATCH",
        url: `/templates/reorder`,
        data: { ids },
        authorization: true,
    });
    return res.data ?? [];
}
