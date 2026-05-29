import Request from "../../Request";
import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";

export interface ValidateDiscountInput {
    code: string;
    plan_id?: string;
}

export default async function validateDiscountCode(
    data: ValidateDiscountInput,
): Promise<DiscountPreview> {
    return await Request<DiscountPreview>({
        method: "POST",
        url: `/subscription/discount/validate`,
        data,
        authorization: true,
    });
}
