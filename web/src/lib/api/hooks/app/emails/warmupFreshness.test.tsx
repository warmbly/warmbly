import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { focusManager, onlineManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import useWarmupLifecycle from "./useWarmupLifecycle";
import useUpdateEmail from "./useUpdateEmail";
import { useCloudLinkMailboxes, useEnrollCloudLinkMailbox } from "../cloudlink/useCloudLink";
import BulkWarmupDialog from "@/components/app/emails/BulkWarmupDialog";
import useSwitchOrganization from "../organizations/useSwitchOrganization";

const fixture = vi.hoisted(() => ({ paused: false, fail: false, partial: false, wait: undefined as Promise<void> | undefined, writes: [] as { url: string; data: unknown }[] }));
vi.mock("@/lib/api/client/Request", () => ({ default: async ({ url, method, data }: { url: string; method: string; data?: unknown }) => {
    if (url.startsWith("/organization/switch/")) return {};
    if (url === "/cloud-link/mailboxes") return { data: [{ id: "mailbox", enrolled: true, cloud: { warmup: { paused: fixture.paused } } }] };
    if (method === "POST" || method === "PATCH") {
        fixture.writes.push({ url, data });
        fixture.paused = true;
        await fixture.wait;
        if (fixture.fail || fixture.partial && url.includes("failed")) throw { code: "cloud_unavailable" };
        return { id: "mailbox" };
    }
    throw new Error(`Unexpected request ${url}`);
} }));

beforeEach(() => { fixture.paused = fixture.fail = fixture.partial = false; fixture.wait = undefined; fixture.writes = []; });
afterEach(() => { cleanup(); focusManager.setFocused(undefined); onlineManager.setOnline(true); });

it.each([false, true])("preserves the selected executor and refreshes reports after enrollment (failure: %s)", async (fail) => {
    fixture.fail = fail;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    const hook = renderHook(() => ({ report: useCloudLinkMailboxes(), enroll: useEnrollCloudLinkMailbox() }), {
        wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>,
    });
    await waitFor(() => expect(hook.result.current.report.data?.[0].cloud?.warmup?.paused).toBe(false));
    await act(async () => { await hook.result.current.enroll.mutateAsync({ id: "mailbox", executor: "instance" }).catch(() => {}); });
    expect(fixture.writes).toContainEqual({ url: "/cloud-link/mailboxes/mailbox/enroll", data: { executor: "instance" } });
    await waitFor(() => expect(hook.result.current.report.data?.[0].cloud?.warmup?.paused).toBe(true));
});

it.each([false, true])("refreshes the Cloud report after a generic lifecycle action without a socket (failure: %s)", async (fail) => {
    fixture.fail = fail;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    client.setQueryData(["cloud-link", "other", "mailboxes"], [{ id: "other" }]);
    const hook = renderHook(() => ({ report: useCloudLinkMailboxes(), life: useWarmupLifecycle("mailbox") }), {
        wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>,
    });
    await waitFor(() => expect(hook.result.current.report.data?.[0].cloud?.warmup?.paused).toBe(false));
    await act(async () => { await hook.result.current.life.mutateAsync("pause").catch(() => {}); });
    await waitFor(() => expect(hook.result.current.report.data?.[0].cloud?.warmup?.paused).toBe(true));
    expect(client.getQueryState(["cloud-link", "other", "mailboxes"])?.isInvalidated).toBe(false);
});

it("refreshes Cloud-owned settings even when the remote write applied before an error", async () => {
    fixture.fail = true;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    const hook = renderHook(() => ({ report: useCloudLinkMailboxes(), update: useUpdateEmail("mailbox", ["warmup_base"]) }), {
        wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>,
    });
    await waitFor(() => expect(hook.result.current.report.data?.[0].cloud?.warmup?.paused).toBe(false));
    await act(async () => { await hook.result.current.update.mutateAsync({ warmup_base: 8 }).catch(() => {}); });
    await waitFor(() => expect(hook.result.current.report.data?.[0].cloud?.warmup?.paused).toBe(true));
});

it.each(["lifecycle", "settings"])("does not restore the old workspace's mailbox after a real cache reset during an in-flight %s write", async (kind) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const hook = renderHook(() => ({ report: useCloudLinkMailboxes(), life: useWarmupLifecycle("mailbox"), update: useUpdateEmail("mailbox", ["warmup_base"]) }), {
        wrapper,
    });
    await waitFor(() => expect(hook.result.current.report.isSuccess).toBe(true));
    let release!: () => void;
    fixture.wait = new Promise<void>((resolve) => { release = resolve; });
    let completion!: Promise<unknown>;
    act(() => { completion = kind === "lifecycle" ? hook.result.current.life.mutateAsync("pause") : hook.result.current.update.mutateAsync({ warmup_base: 8 }); });
    await waitFor(() => expect(fixture.paused).toBe(true));
    hook.unmount();
    const switcher = renderHook(useSwitchOrganization, { wrapper });
    await act(async () => { await switcher.result.current.mutateAsync("other"); });
    client.setQueryData(["organizations", "current"], { id: "other" });
    client.setQueryData(["cloud-link", "other", "mailboxes"], [{ id: "other" }]);
    await act(async () => { release(); await completion; });
    expect(client.getQueryData(["emails", "mailbox"])).toBeUndefined();
    expect(client.getQueryState(["cloud-link", "workspace", "mailboxes"])).toBeUndefined();
    expect(client.getQueryState(["cloud-link", "other", "mailboxes"])?.isInvalidated).toBe(false);
});

it.each(["focus", "reconnect"])("refreshes stale Cloud reports on %s even when global focus refresh is disabled", async (event) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false } } });
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    const key = ["cloud-link", "workspace", "mailboxes"];
    const hook = renderHook(useCloudLinkMailboxes, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });
    await waitFor(() => expect(hook.result.current.data?.[0].cloud?.warmup?.paused).toBe(false));
    fixture.paused = true;
    client.setQueryData(key, client.getQueryData(key), { updatedAt: Date.now() - 20_000 });
    act(() => {
        if (event === "focus") { focusManager.setFocused(false); focusManager.setFocused(true); }
        else { onlineManager.setOnline(false); onlineManager.setOnline(true); }
    });
    await waitFor(() => expect(hook.result.current.data?.[0].cloud?.warmup?.paused).toBe(true));
});

it("refreshes Cloud reports after a partially successful bulk settings and start operation", async () => {
    fixture.partial = true;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["organizations", "current"], { id: "workspace" });
    const complete = vi.fn();
    const hook = renderHook(useCloudLinkMailboxes, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });
    await waitFor(() => expect(hook.result.current.data?.[0].cloud?.warmup?.paused).toBe(false));
    render(<QueryClientProvider client={client}><BulkWarmupDialog open ids={["mailbox", "failed"]} onClose={() => {}} onComplete={complete} /></QueryClientProvider>);
    fireEvent.click(screen.getByRole("button", { name: "Start warmup" }));
    await waitFor(() => expect(complete).toHaveBeenCalledOnce());
    expect(hook.result.current.data?.[0].cloud?.warmup?.paused).toBe(true);
});
