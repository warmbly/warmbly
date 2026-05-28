import type { DNSVerification } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export interface DNSVerifyInput {
    domain: string;
    dkim_selector?: string;
    tracking_cname?: string;
}

export default async function verifyDNS(input: DNSVerifyInput): Promise<DNSVerification> {
    return await Request<DNSVerification>({
        method: "POST",
        url: "/integrations/dns/verify",
        data: input,
        authorization: true,
    });
}
