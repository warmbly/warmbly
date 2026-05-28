import type { DNSVerification } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function listDNSVerifications(): Promise<{ verifications: DNSVerification[] }> {
    return await Request<{ verifications: DNSVerification[] }>({
        method: "GET",
        url: "/integrations/dns/verifications",
        authorization: true,
    });
}
