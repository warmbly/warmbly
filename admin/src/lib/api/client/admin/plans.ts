// /admin/plans — plan catalog.

import { Request } from "@/lib/api/client";
import type { Plan, UpdatePlanRequest } from "@/lib/api/models/admin";

export function listPlans(): Promise<{ data: Plan[] }> {
    return Request({
        method: "GET",
        url: "/admin/plans",
        authorization: true,
    });
}

export function updatePlan(
    id: string,
    body: UpdatePlanRequest,
): Promise<Plan> {
    return Request({
        method: "PATCH",
        url: `/admin/plans/${id}`,
        authorization: true,
        data: body,
    });
}
