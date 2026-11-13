import React from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { queryCompanyEnrich } from "@/lib/api/client/app/integrations/companyenrich";
import type { CompanyEnrichResult } from "@/lib/api/models/app/integrations/CompanyEnrich";
import ResearchPanel from "./ResearchPanel";
import Results from "./Results";

vi.mock("@/lib/api/client/app/integrations/companyenrich", () => ({ queryCompanyEnrich: vi.fn(), enrichCompanyEnrichContact: vi.fn() }));

function wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: 3, retryDelay: 1 } } })}>{children}</QueryClientProvider>;
}
function enterDomain() {
    fireEvent.change(screen.getByLabelText("Company domain"), { target: { value: "example.com" } });
}

describe("CompanyEnrich paid request UI", () => {
    beforeEach(() => vi.clearAllMocks());
    it("requires an explicit click, displays results, and clears them when inputs change", async () => {
        vi.mocked(queryCompanyEnrich).mockResolvedValue({ data: { name: "Example company", id: "9223372036854775807", extra: { retained: true } }, credits_consumed: 0 });
        render(<ResearchPanel connectionId="connection" enabled />, { wrapper });
        expect(queryCompanyEnrich).not.toHaveBeenCalled();
        expect(screen.getByRole("button", { name: "Run one request" })).toBeEnabled();
        enterDomain();
        fireEvent.click(screen.getByRole("button", { name: "Run one request" }));
        await screen.findByRole("heading", { name: "Example company" });
        expect(queryCompanyEnrich).toHaveBeenCalledTimes(1);
        expect(queryCompanyEnrich).toHaveBeenCalledWith("connection", { operation: "enrich_company", query: { domain: "example.com" } });
        expect(screen.getByText("0 credits consumed")).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Run one request" })).toBeEnabled();
        fireEvent.change(screen.getByLabelText("Company domain"), { target: { value: "new.com" } });
        expect(screen.queryByRole("heading", { name: "Example company" })).not.toBeInTheDocument();
    });

    it("does not retry a failed paid request even when the app default retries mutations", async () => {
        vi.mocked(queryCompanyEnrich).mockRejectedValue(new Error("Provider unavailable"));
        render(<ResearchPanel connectionId="connection" enabled />, { wrapper });
        enterDomain(); fireEvent.click(screen.getByRole("button", { name: "Run one request" }));
        await screen.findByRole("alert");
        expect(queryCompanyEnrich).toHaveBeenCalledTimes(1);
        expect(screen.getByRole("button", { name: "Run one request" })).toBeEnabled();
    });

    it("blocks double submission and never displays a previous connection's late response", async () => {
        let finish!: (value: CompanyEnrichResult) => void;
        vi.mocked(queryCompanyEnrich).mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
        const view = render(<ResearchPanel key="one" connectionId="one" enabled />, { wrapper });
        enterDomain();
        const button = screen.getByRole("button", { name: "Run one request" });
        fireEvent.click(button); fireEvent.click(button);
        await waitFor(() => expect(queryCompanyEnrich).toHaveBeenCalledTimes(1));
        expect(screen.getByLabelText("Company domain")).toBeDisabled();
        view.rerender(<ResearchPanel key="two" connectionId="two" enabled />);
        await act(async () => finish({ data: { name: "Stale result" } }));
        expect(screen.queryByText("Stale result")).not.toBeInTheDocument();
    });

    it("renders zero counts, empty pages and arbitrary metadata without losing the full payload", () => {
        const view = render(<Results result={{ data: 0 }} />);
        expect(screen.getByText("0", { selector: "div" })).toBeInTheDocument();
        view.rerender(<Results result={{ data: { items: [], page: 2, totalItems: 0, scores: {}, future: { id: "9223372036854775807" } } }} />);
        expect(screen.getByText("No results returned for this request.")).toBeInTheDocument();
        expect(screen.getByText(/"9223372036854775807"/)).toBeInTheDocument();
    });

    it("shows snake_case person names and nested similarity scores on readable result cards", () => {
        render(<Results result={{ data: { items: [{ id: "9223372036854775807", first_name: "Jane", last_name: "Doe", position: "Founder" }], metadata: { scores: { "9223372036854775807": 0.95 } } } }} />);
        expect(screen.getByRole("heading", { name: "Jane Doe" })).toBeInTheDocument();
        expect(screen.getByText("0.95", { selector: "dd" })).toBeInTheDocument();
    });
});
