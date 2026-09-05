import Request from "../../Request";

// Mirrors handler.EnterpriseInquiryRequest. Company, contact name and a valid
// contact email are required server-side; the rest are optional. The previous
// shape ({name, email, company, message}) matched no field the endpoint binds,
// so every call was rejected as an invalid body.
export interface EnterpriseInquiryInput {
    company_name: string;
    contact_name: string;
    contact_email: string;
    estimated_volume?: number;
    team_size?: number;
    notes?: string;
}

export default async function enterpriseInquiry(
    data: EnterpriseInquiryInput,
): Promise<{ message: string; inquiry_id: string }> {
    return await Request<{ message: string; inquiry_id: string }>({
        method: "POST",
        url: `/subscription/enterprise-inquiry`,
        data,
        authorization: true,
    })
}
