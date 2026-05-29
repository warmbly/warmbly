import Request from "../../Request";

export interface CreateCheckoutInput {
    price_id: string;
    success_url: string;
    cancel_url: string;
    // Optional discount/promo code applied at checkout.
    discount_code?: string;
}

export default async function createCheckoutSession(
    data: CreateCheckoutInput,
): Promise<{ session_id: string; checkout_url: string }> {
    return await Request<{ session_id: string; checkout_url: string }>({
        method: "POST",
        url: `/subscription/checkout`,
        data,
        authorization: true,
    });
}
