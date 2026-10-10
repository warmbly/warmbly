import Request from "../Request";

export interface ProductTourResponse {
    product_tour_completed_at: string;
}

// Records that the product tour was finished or skipped. Repeating it keeps the first time.
export default async function completeProductTour(): Promise<ProductTourResponse> {
    return await Request<ProductTourResponse>({
        method: "PUT",
        url: "/auth/me/product-tour",
        authorization: true,
    });
}
