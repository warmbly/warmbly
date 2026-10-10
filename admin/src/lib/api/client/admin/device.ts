import { Request } from "@/lib/api/client";

export interface DeviceDescription {
    request: {
        client_name: string;
        instance_url: string;
        expires_at: string;
        status: "pending";
        consent_token: string;
    };
    requested_access: string;
    admin_permissions: number;
}

export function describeAdminDevice(userCode: string): Promise<DeviceDescription> {
    return Request({ method: "POST", url: "/admin/auth/device/describe", authorization: true, data: { user_code: userCode } });
}

export function decideAdminDevice(userCode: string, consentToken: string, decision: "approved" | "denied"): Promise<{ status: "approved" | "denied" }> {
    return Request({
        method: "POST", url: "/admin/auth/device/decide", authorization: true, skipReauthPrompt: true,
        data: { user_code: userCode, consent_token: consentToken, decision },
    });
}
