import Request from "../../Request";
import type Category from "@/lib/api/models/app/Category";

export default async function createCategory(title: string): Promise<Category> {
    return await Request<Category>({
        method: "POST",
        url: `/categories`,
        data: {
            title,
        },
        authorization: true,
    })
}
