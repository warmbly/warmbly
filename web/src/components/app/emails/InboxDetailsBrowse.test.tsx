import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ComponentProps } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { ConfirmContext } from "@/hooks/context/confirm";
import { useAppStore } from "@/stores/useAppStore";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type { PoolLinkMailboxState } from "@/lib/api/models/app/cloudlink/CloudLink";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import type { MailboxTab } from "@/lib/browse-accounts-analytics";
import { BROWSE_STORAGE_PREFIX } from "@/lib/browseState";
import InboxDetails from "./InboxDetails";

const cloudState = vi.hoisted(() => ({ loading: false, unavailable: false, managed: false, connected: false, report: undefined as PoolLinkMailboxState | undefined, authLoading: false, confirm: vi.fn() }));

vi.mock("@/lib/api/client/Request", () => ({ default: () => new Promise(() => {}) }));
vi.mock("@/hooks/PresenceProvider", () => ({ usePresenceResource: () => {} }));
vi.mock("@/components/app/presence/ResourceViewers", () => ({ default: () => null }));
vi.mock("@/components/app/advisor/AdvisorStrip", () => ({ default: () => null }));
vi.mock("@/hooks/useCloudPool", () => ({ default: () => ({ connected: cloudState.connected, selfHosted: false, loading: cloudState.loading, unavailable: cloudState.unavailable, isEnrolled: () => cloudState.connected, rowFor: () => ({ enrolled: cloudState.connected, cloud: cloudState.report, managed: cloudState.managed }) }) }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ isLoading: cloudState.authLoading, isError: false }) }));
vi.mock("./SendingBehaviorTab", () => ({ default: () => <p>Sending behavior</p> }));

const mailbox: Inbox = {
    id: "a", email: "alice@a.test", name: "Alice", signature_plain: "", signature_html: "", signature_sync: false, signature_code: false,
    send_as_email: "", tags: [], provider: "smtp_imap", status: "active", last_synced_at: new Date(), campaign_limit: 50, min_wait_time: 60,
    reply_to: "", save_to_sent: false, tracking_domain: "", tracking_domain_verified: false,
    auth_state: "unknown", auth_spf: false, auth_dkim: false, auth_dmarc: false,
    warmup_base: 5, warmup_max: 20, warmup_increase: 2, warmup_reply_rate: 30, created_at: new Date(), updated_at: new Date(),
};
let client: QueryClient;
function Page({ id = "a", initialTab, tabIntentKey = 0, onTabIntentConsumed }: { id?: string; initialTab?: MailboxTab; tabIntentKey?: number; onTabIntentConsumed?: () => void }) {
    const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return (
        <QueryClientProvider client={client}>
            <UserContext.Provider value={profile}>
                <ConfirmContext.Provider value={{ show: cloudState.confirm, setLoading: vi.fn(), setShow: vi.fn() }}>
                    <InboxDetails emails={[mailbox, { ...mailbox, id: "b", email: "bob@b.test" }]} view={id} setView={() => {}} initialTab={initialTab} tabIntentKey={tabIntentKey} onTabIntentConsumed={onTabIntentConsumed} />
                </ConfirmContext.Provider>
            </UserContext.Provider>
        </QueryClientProvider>
    );
}
const tab = (name: string) => screen.getByRole("button", { name });

describe("mailbox drawer browse persistence", () => {
    beforeEach(() => {
        cloudState.loading = cloudState.unavailable = cloudState.managed = cloudState.authLoading = false;
        cloudState.connected = false;
        cloudState.report = undefined;
        cloudState.confirm.mockReset();
        HTMLElement.prototype.scrollBy = vi.fn();
        sessionStorage.clear();
        client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("shows Cloud Today values instead of the local zero ramp", () => {
        cloudState.connected = true;
        cloudState.report = { status: "active", sent_today: 5, warmup: { enabled: true, paused: false, current_volume: 5, target_volume: 12, days_active: 3 } } as PoolLinkMailboxState;
        client.setQueryData(["analytics", "accounts", "a"], { warmup_status: { enabled: true, current_volume: 0, target_volume: 0, days_active: 0, partner_limit: { ramp_target: 0, reachable: 0 } } } as AccountStatus);
        render(<Page initialTab="warmup" />);
        expect(screen.getByText(/Sending/, { selector: "span" })).toHaveTextContent("Sending 5 of 12 · day 3");
        expect(screen.queryByText(/only 0 partners/)).not.toBeInTheDocument();
    });

    it("uses Cloud sends and ramp details on Overview even without a local warmup start", () => {
        cloudState.connected = true;
        cloudState.report = { status: "active", sent_today: 7, warmup: { enabled: true, paused: false, current_volume: 0, target_volume: 15, days_active: 4, max_volume: 30, reply_rate: 25 } } as PoolLinkMailboxState;
        render(<Page />);
        const stat = screen.getByText("Warmup today").parentElement;
        expect(stat).toHaveTextContent("7");
        expect(stat).toHaveTextContent("Warmbly Cloud · target 15");
        expect(screen.getByText("Days warming").parentElement).toHaveTextContent("4");
    });

    it("shows unavailable Cloud stats rather than a local zero when the remote report is missing", () => {
        cloudState.connected = true;
        client.setQueryData(["analytics", "accounts", "a"], { daily_usage: { warmup_sent: 0 }, warmup_status: { current_volume: 0, target_volume: 0 } } as AccountStatus);
        render(<Page />);
        const stat = screen.getByText("Warmup today").parentElement;
        expect(stat).toHaveTextContent("—");
        expect(stat).toHaveTextContent("warmup status unavailable");
        expect(stat).not.toHaveTextContent("target 0");
    });

    it("restores drawer tabs on remount and isolates mailbox resources", () => {
        const page = render(<Page />);
        fireEvent.click(tab("Analytics"));
        page.unmount();
        const refreshed = render(<Page />);
        expect(tab("Analytics")).toHaveAttribute("data-active", "true");
        refreshed.rerender(<Page id="b" />);
        expect(tab("Overview")).toHaveAttribute("data-active", "true");
        refreshed.rerender(<Page />);
        expect(tab("Analytics")).toHaveAttribute("data-active", "true");
    });

    it("honors repeated explicit tab intents without discarding mailbox drafts", () => {
        const first = render(<Page />);
        fireEvent.click(tab("Sending"));
        first.unmount();
        const page = render(<Page initialTab="analytics" />);
        expect(tab("Analytics")).toHaveAttribute("data-active", "true");
        fireEvent.click(tab("Warmup"));
        fireEvent.change(screen.getByLabelText(/Shared daily send limit/), { target: { value: "80" } });
        expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
        page.rerender(<Page initialTab="analytics" tabIntentKey={1} />);
        expect(tab("Analytics")).toHaveAttribute("data-active", "true");
        expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
        fireEvent.click(tab("Warmup"));
        expect(screen.getByLabelText(/Shared daily send limit/)).toHaveValue(80);
        page.rerender(<Page initialTab="analytics" tabIntentKey={2} />);
        expect(tab("Analytics")).toHaveAttribute("data-active", "true");
    });

    it("consumes explicit navigation so another workspace restores its own tab", () => {
        sessionStorage.setItem(`${BROWSE_STORAGE_PREFIX}account-user:other:emails.mailbox.a.tab`, JSON.stringify({ value: "sending" }));
        const consumed = vi.fn();
        const page = render(<Page initialTab="analytics" onTabIntentConsumed={consumed} />);
        expect(consumed).toHaveBeenCalledOnce();
        page.rerender(<Page />);
        act(() => useAppStore.setState({ currentOrganization: { id: "other", name: "Other", role: "owner" } }));
        expect(tab("Sending")).toHaveAttribute("data-active", "true");
    });

    it.each(["loading", "unavailable", "authLoading"] as const)("defers disconnect terms while Cloud ownership is %s", (unknown) => {
        cloudState[unknown] = true;
        const page = render(<Page initialTab="settings" />);
        expect(screen.getByRole("button", { name: "Disconnect…" })).toBeDisabled();
        expect(screen.queryByText(/The stored SMTP and IMAP credentials are destroyed/)).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Disconnect…" }));
        expect(cloudState.confirm).not.toHaveBeenCalled();
        cloudState[unknown] = false;
        cloudState.managed = true;
        page.rerender(<Page initialTab="settings" />);
        fireEvent.click(screen.getByRole("button", { name: "Disconnect…" }));
        expect(cloudState.confirm).toHaveBeenCalledWith(expect.stringContaining("stop its Cloud warmup sending and receiving"), expect.any(Function));
        expect(cloudState.confirm).toHaveBeenCalledWith(expect.stringContaining("mailbox, sign-in and history stay in your Warmbly Cloud workspace"), expect.any(Function));
    });
});
