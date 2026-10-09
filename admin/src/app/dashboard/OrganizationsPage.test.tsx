import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { Column } from "@/components/data/DataTable";
import type { AdminOrgListItem } from "@/lib/api/models/admin";
import OrganizationsPage from "./OrganizationsPage";

const state = vi.hoisted(() => ({ rows: [] as AdminOrgListItem[], columns: [] as Column<AdminOrgListItem>[] }));
vi.mock("@tanstack/react-query", () => ({
    useQuery: () => ({ data: { data: state.rows, pagination: { total: state.rows.length } }, isLoading: false }),
    keepPreviousData: undefined,
}));
vi.mock("@/components/data/DataTable", () => ({
    DataTable: ({ columns, rows }: { columns: Column<AdminOrgListItem>[]; rows: AdminOrgListItem[] }) => {
        state.columns = columns;
        const channel = columns.find((column) => column.id === "channel")!;
        return <>{rows.map((row) => <div key={row.id}>{channel.cell(row)}</div>)}</>;
    },
}));

function render(acquisition: Partial<AdminOrgListItem>) {
    state.rows = [{ id: "workspace", ...acquisition } as AdminOrgListItem];
    const html = renderToStaticMarkup(<MemoryRouter><OrganizationsPage /></MemoryRouter>);
    return new DOMParser().parseFromString(html, "text/html");
}

describe("organization signup channel", () => {
    it("shows a referrer-only signup instead of calling it direct", () => {
        const document = render({ referrer_host: "www.google.com" });
        expect(document.body.textContent).toContain("www.google.com");
        expect(state.columns.find((column) => column.id === "channel")?.defaultHidden).not.toBe(true);
    });

    it("keeps the campaign, referrer and landing path visible and exportable together", () => {
        const document = render({ utm_source: "reddit", utm_medium: "social", utm_campaign: "launch", referrer_host: "www.reddit.com", landing_path: "/pricing" });
        expect(document.body.textContent).toContain("redditsocialwww.reddit.com/pricing");
        expect(document.querySelector('[title="www.reddit.com · launch · /pricing"]')).not.toBeNull();
        expect(state.columns.find((column) => column.id === "channel")?.csv?.(state.rows[0])).toBe("reddit | social | launch | www.reddit.com | /pricing");
    });

    it("does not invent direct or an external source for unknown or landing-only acquisition", () => {
        for (const acquisition of [{}, { landing_path: "/pricing" }]) {
            const document = render(acquisition);
            expect(document.body.textContent).toContain("Unknown");
            expect(document.body.textContent).not.toContain("direct");
        }
    });
});
