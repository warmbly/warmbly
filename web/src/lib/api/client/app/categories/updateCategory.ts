import Request from "../../Request";
import type Category from "@/lib/api/models/app/Category";

export default async function updateCategory(id: string, category: Partial<Category>): Promise<Category> {
    return await Request<Category>({
        method: "PATCH",
        url: `/categories/${id}`,
        data: category,
        authorization: true,
    })
}
