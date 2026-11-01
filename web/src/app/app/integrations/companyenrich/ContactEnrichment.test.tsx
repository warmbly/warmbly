import React from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { enrichCompanyEnrichContact } from "@/lib/api/client/app/integrations/companyenrich";
import searchContacts from "@/lib/api/client/app/contacts/searchContacts";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type { CompanyEnrichContactResult } from "@/lib/api/models/app/integrations/CompanyEnrich";
import ContactEnrichment from "./ContactEnrichment";

vi.mock("@/lib/api/client/app/integrations/companyenrich", () => ({ queryCompanyEnrich: vi.fn(), enrichCompanyEnrichContact: vi.fn() }));
vi.mock("@/lib/api/client/app/contacts/searchContacts", () => ({ default: vi.fn() }));
const contact: Contact = { id: "00000000-0000-4000-8000-000000000001", first_name: "Jane", last_name: "Doe", email: "jane@example.com", company: "", phone: "", custom_fields: {}, subscribed: true, campaigns: [], categories: [], created_at: new Date(), updated_at: new Date() };
function wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: 3, retryDelay: 1 }, queries: { retry: false } } })}>{children}</QueryClientProvider>;
}
async function chooseContact() {
    fireEvent.click(screen.getByRole("button", { name: "Choose an existing contact" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Jane Doe/ }));
}

describe("CompanyEnrich saved contact enrichment", () => {
    beforeEach(() => {
        vi.resetAllMocks();
        vi.mocked(searchContacts).mockResolvedValue({ data: [contact], pagination: { total: 1, has_more: false, next_cursor: null } });
    });
    it("only enriches the selected saved contact after a click and always fills blanks", async () => {
        vi.mocked(enrichCompanyEnrichContact).mockResolvedValue({ contact: { ...contact, company: "Example" }, updated_fields: ["company"], data: { first_name: "Jane", last_name: "Doe" }, credits_consumed: 1 });
        render(<ContactEnrichment connectionId="connection" enabled />, { wrapper });
        expect(searchContacts).not.toHaveBeenCalled();
        expect(enrichCompanyEnrichContact).not.toHaveBeenCalled();
        expect(screen.getByRole("button", { name: "Enrich contact" })).toBeDisabled();
        await chooseContact();
        expect(enrichCompanyEnrichContact).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole("button", { name: "Enrich contact" }));
        expect(await screen.findByRole("status")).toHaveTextContent("Updated Jane Doe: company.");
        expect(enrichCompanyEnrichContact).toHaveBeenCalledExactlyOnceWith("connection", { contact_id: contact.id, kind: "person", overwrite: false });
        fireEvent.click(screen.getByRole("radio", { name: /Company details/ }));
        expect(screen.queryByRole("status")).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Enrich contact" }));
        await waitFor(() => expect(enrichCompanyEnrichContact).toHaveBeenLastCalledWith("connection", { contact_id: contact.id, kind: "company", overwrite: false }));
    });
    it("does not load contacts or permit paid calls when disabled", () => {
        render(<ContactEnrichment connectionId="connection" enabled={false} />, { wrapper });
        expect(screen.getByRole("button", { name: "Choose an existing contact" })).toBeDisabled();
        expect(screen.getByRole("button", { name: "Enrich contact" })).toBeDisabled();
        expect(searchContacts).not.toHaveBeenCalled();
        expect(enrichCompanyEnrichContact).not.toHaveBeenCalled();
    });
    it("does not retry enrichment failures", async () => {
        vi.mocked(enrichCompanyEnrichContact).mockRejectedValue(new Error("Provider unavailable"));
        render(<ContactEnrichment connectionId="connection" enabled />, { wrapper });
        await chooseContact();
        fireEvent.click(screen.getByRole("button", { name: "Enrich contact" }));
        await screen.findByRole("alert");
        expect(enrichCompanyEnrichContact).toHaveBeenCalledTimes(1);
    });
    it("blocks double submission and discards a response after moving to another connection", async () => {
        let finish!: (value: CompanyEnrichContactResult) => void;
        vi.mocked(enrichCompanyEnrichContact).mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
        const view = render(<ContactEnrichment key="one" connectionId="one" enabled />, { wrapper });
        await chooseContact();
        const button = screen.getByRole("button", { name: "Enrich contact" });
        fireEvent.click(button); fireEvent.click(button);
        await waitFor(() => expect(enrichCompanyEnrichContact).toHaveBeenCalledTimes(1));
        expect(screen.getByRole("button", { name: "Choose an existing contact" })).toBeDisabled();
        view.rerender(<ContactEnrichment key="two" connectionId="two" enabled />);
        await act(async () => finish({ contact, updated_fields: ["company"], data: { name: "Stale company" } }));
        expect(screen.queryByText("Stale company")).not.toBeInTheDocument();
        expect(screen.queryByRole("status")).not.toBeInTheDocument();
    });
});
