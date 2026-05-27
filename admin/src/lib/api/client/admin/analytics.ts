// /admin/analytics/*

import { Request } from "@/lib/api/client";
import type { PlatformOverview } from "@/lib/api/models/admin";

export function getPlatformOverview(): Promise<PlatformOverview> {
    return Request({
        method: "GET",
        url: "/admin/analytics/overview",
        authorization: true,
    });
}
