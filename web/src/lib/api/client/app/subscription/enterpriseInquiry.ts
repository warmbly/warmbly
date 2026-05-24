import Request from "../../Request";

export default async function enterpriseInquiry(data: { name: string; email: string; company: string; message?: string }): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/subscription/enterprise-inquiry`,
        data,
        authorization: true,
    })
}
