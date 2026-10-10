import type { DeviceDescription } from "@/lib/api/client/admin/device";

export function instanceIdentity(value: string): string | null {
    try {
        const url = new URL(value);
        if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.search || url.hash) return null;
        return url.toString().replace(/\/+$/, "");
    } catch { return null; }
}

export function validDeviceDescription(value: DeviceDescription, backend: string): boolean {
    const request = value?.request;
    const configured = instanceIdentity(backend);
    return !!request && request.status === "pending" && !!configured && instanceIdentity(request.instance_url) === configured &&
        typeof request.client_name === "string" && request.client_name.length > 0 && request.client_name.length <= 64 &&
        /^[A-Za-z0-9_-]{43}$/.test(request.consent_token) && Number.isFinite(Date.parse(request.expires_at)) &&
        value.requested_access === "Your live platform administrator permissions through a separate revocable session" &&
        Number.isSafeInteger(value.admin_permissions) && value.admin_permissions > 0 && value.admin_permissions <= 0xffffffff;
}
