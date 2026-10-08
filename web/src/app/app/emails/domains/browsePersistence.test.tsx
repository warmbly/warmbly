import { act, fireEvent, render, screen } from "@testing-library/react";
import { useCallback, useState, type ComponentProps, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import type { SendingDomain } from "@/lib/api/models/app/emails/SendingDomain";
import type { DomainTab } from "@/components/app/emails/domains/SendingDomainDrawer";
import DomainsPage from "./page";

const navigation = vi.hoisted(() => ({ initial: "", consumed: "" }));
vi.mock("@/hooks/useSearchParams", () => ({
    useSearchParams: () => {
        const [params, setParams] = useState(() => new URLSearchParams(navigation.initial));
        const set = useCallback((next: URLSearchParams | ((prev: URLSearchParams) => URLSearchParams)) => {
            setParams((prev) => {
                const result = typeof next === "function" ? next(prev) : next;
                navigation.consumed = result.toString();
                return result;
            });
        }, []);
        return [params, set];
    },
}));
const domains: SendingDomain[] = ["a.test", "b.test"].map((domain) => ({
    domain, mailboxes: 1, mail_hosts: [], auth_state: "passing", auth_spf: true, auth_dkim: true, auth_dmarc: true, tracking_domains: [],
}));
vi.mock("@/lib/api/hooks/app/emails/useSendingDomains", () => ({
    useSendingDomains: () => ({ data: { data: domains }, isLoading: false }),
}));
vi.mock("@tanstack/react-router", () => ({ Link: ({ children }: { children: ReactNode }) => <a>{children}</a> }));
vi.mock("@/components/app/emails/domains/RedirectRescue", () => ({ default: () => null }));
vi.mock("@/components/app/emails/domains/BulkSetupDialog", () => ({ default: () => null }));
vi.mock("@/components/app/emails/domains/DomainSelectionBar", () => ({ default: () => null }));
vi.mock("@/components/app/emails/domains/SendingDomainDrawer", () => ({
    default: ({ open, tab, setTab, onClose }: { open: string; tab: DomainTab; setTab: (tab: DomainTab) => void; onClose: () => void }) => open ? (
        <section aria-label="Domain drawer">
            <output aria-label="Domain tab">{tab}</output>
            <button onClick={() => setTab("tracking")}>Choose tracking</button>
            <button onClick={() => setTab("overview")}>Choose overview</button>
            <button onClick={onClose}>Close domain</button>
        </section>
    ) : null,
}));

function Page() {
    const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={profile}><DomainsPage /></UserContext.Provider>;
}

describe("domain drawer browse persistence", () => {
    beforeEach(() => {
        sessionStorage.clear();
        navigation.initial = "";
        navigation.consumed = "";
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("remembers per-domain tabs without reopening drawers on page refresh", () => {
        const page = render(<Page />);
        fireEvent.click(screen.getByText("a.test"));
        fireEvent.click(screen.getByRole("button", { name: "Choose tracking" }));
        fireEvent.click(screen.getByRole("button", { name: "Close domain" }));
        fireEvent.click(screen.getByText("b.test"));
        expect(screen.getByLabelText("Domain tab")).toHaveTextContent("overview");
        fireEvent.click(screen.getByRole("button", { name: "Close domain" }));
        page.unmount();
        render(<Page />);
        expect(screen.queryByRole("region", { name: "Domain drawer" })).not.toBeInTheDocument();
        fireEvent.click(screen.getByText("a.test"));
        expect(screen.getByLabelText("Domain tab")).toHaveTextContent("tracking");
    });

    it("prioritizes and consumes a URL tab intent, then remembers the user's reset", () => {
        const key = `${BROWSE_STORAGE_PREFIX}account-user:workspace:emails.domain.a.test.tab`;
        sessionStorage.setItem(key, JSON.stringify({ value: "tracking" }));
        navigation.initial = "domain=a.test&tab=redirect&keep=1";
        const page = render(<Page />);
        expect(screen.getByLabelText("Domain tab")).toHaveTextContent("redirect");
        expect(navigation.consumed).toBe("keep=1");
        fireEvent.click(screen.getByRole("button", { name: "Choose overview" }));
        page.unmount();
        navigation.initial = "";
        render(<Page />);
        fireEvent.click(screen.getByText("a.test"));
        expect(screen.getByLabelText("Domain tab")).toHaveTextContent("overview");
    });

    it("does not replay a consumed URL tab intent on workspace switching", () => {
        sessionStorage.setItem(`${BROWSE_STORAGE_PREFIX}account-user:other:emails.domain.a.test.tab`, JSON.stringify({ value: "tracking" }));
        navigation.initial = "domain=a.test&tab=redirect";
        render(<Page />);
        expect(screen.getByLabelText("Domain tab")).toHaveTextContent("redirect");
        act(() => useAppStore.setState({ currentOrganization: { id: "other", name: "Other", role: "owner" } }));
        expect(screen.getByLabelText("Domain tab")).toHaveTextContent("tracking");
    });
});
