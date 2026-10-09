import { describe, expect, it } from "vitest";
import { AdminPerm } from "@/lib/auth/permissions";
import type { MonitoringSource } from "@/lib/api/client/admin/monitoring";
import { canMonitor, investigationPath, MONITORING_SOURCES, observationIsOld, permittedSource } from "./monitoring";

describe("monitoring permission and freshness boundaries", () => {
    it.each(Object.entries(MONITORING_SOURCES))("requires the native source bit for %s", (id, { permission }) => {
        const source = { id, availability: "fresh", metrics: [{ count: 42 }] } as MonitoringSource;
        expect(permittedSource(source, AdminPerm.ViewAnalytics | permission)).toBe(source);
        if (permission !== AdminPerm.ViewAnalytics) {
            expect(permittedSource(source, AdminPerm.ViewAnalytics)).toMatchObject({ availability: "unavailable", reason: "permission_denied", metrics: [], measured_scopes: null, expected_scopes: null });
        }
    });

    it("fails closed for unknown masks and unsupported destinations", () => {
        expect(canMonitor(undefined, AdminPerm.ViewAnalytics)).toBe(false);
        for (const path of ["https://external.invalid/", "//external.invalid/", "/workers/secret@example.com", "/sends?tab=failures&token=secret", "/jobs#payload"]) {
            expect(investigationPath(path, 0xffffff)).toBeNull();
        }
    });

    it("accepts known native destinations only with their separate page permission", () => {
        for (const [path, permission] of [["/sends?tab=dead-letters", AdminPerm.ViewCampaigns], ["/sync?state=backfilling", AdminPerm.ViewUsers], ["/mailboxes", AdminPerm.ViewUsers], ["/fleet", AdminPerm.ViewWorkers], ["/workers/1a64feee-dee2-41df-a7f5-a85816ba8585", AdminPerm.ViewWorkers], ["/organizations/1a64feee-dee2-41df-a7f5-a85816ba8585", AdminPerm.ViewOrganizations], ["/jobs", AdminPerm.ViewAnalytics], ["/configuration?tab=notifications", AdminPerm.ManageSettings]] as const) {
            expect(investigationPath(path, permission)).toBe(path);
            expect(investigationPath(path, 0)).toBeNull();
        }
    });

    it("does not call missing, invalid, future or old observation clocks fresh", () => {
        const now = Date.now();
        for (const value of [null, "not-a-time", new Date(now - 121_000).toISOString(), new Date(now + 61_000).toISOString()]) expect(observationIsOld(value, now)).toBe(true);
        expect(observationIsOld(new Date(now - 60_000).toISOString(), now)).toBe(false);
    });
});
