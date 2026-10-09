import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type EmailSync from "@/lib/api/models/app/emails/SyncState";
import SyncStatusCard from "./SyncStatusCard";

const fixtures = vi.hoisted(() => ({ data: undefined as EmailSync | undefined }));
vi.mock("@/lib/api/hooks/app/emails/useSync", () => ({ default: () => ({ data: fixtures.data, isPending: false }) }));
vi.mock("@/lib/api/hooks/app/emails/useUpdateSyncSkipFolders", () => ({ default: () => ({}) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({}) }));

describe("truthful mailbox sync status", () => {
    beforeEach(() => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date("2026-10-09T12:00:00Z"));
        fixtures.data = {
            state: { backfill_status: "complete", backfill_synced: 42, deferred: 0 },
            policy: { backfill_days: 30, backfill_messages: 2000, daily_messages: 500, org_daily_messages: 2000 },
            folders: [], skip_folders: [],
        };
    });
    afterEach(() => { cleanup(); vi.useRealTimers(); });
    const draw = () => render(<SyncStatusCard mailboxId="mailbox" />);

    it("does not treat a completed historical import as proof of live health", () => {
        draw();
        expect(screen.getByText("Recent-mail import complete")).toBeInTheDocument();
        expect(screen.getByText("No successful sync check reported yet.")).toBeInTheDocument();
        expect(screen.getByText(/Import completion does not confirm live sync is current/)).toBeInTheDocument();
        expect(screen.queryByText(/Up to date/i)).not.toBeInTheDocument();
    });

    it("ages a recent successful check into stale evidence without a new server payload", () => {
        fixtures.data!.state!.last_synced_at = new Date("2026-10-09T11:59:00Z");
        draw();
        expect(screen.getByText("Last successful sync 1 min ago.")).toBeInTheDocument();
        act(() => { vi.advanceTimersByTime(21 * 60_000); });
        expect(screen.getByText("No recent successful sync check")).toBeInTheDocument();
        expect(screen.getByText(/Recovery is not yet confirmed by a recent successful check/)).toBeInTheDocument();
        expect(screen.queryByText(/Up to date/i)).not.toBeInTheDocument();
    });

    it("shows ongoing recovery from the existing cursor instead of historical completion", () => {
        fixtures.data!.state!.backfill_cursor = { google_recovery: { history_id: 100, since: new Date(), messages_done: false } };
        draw();
        expect(screen.getByText("Recovering mailbox sync")).toBeInTheDocument();
        expect(screen.queryByText("Recent-mail import complete")).not.toBeInTheDocument();
    });

    it("keeps deferred evidence visible after the budget hold expires", () => {
        fixtures.data!.state!.deferred = 5;
        fixtures.data!.state!.throttled_until = new Date("2026-10-09T12:01:00Z");
        const view = draw();
        expect(screen.getByText(/Waiting on the sync budget until/)).toBeInTheDocument();
        act(() => { vi.advanceTimersByTime(2 * 60_000); });
        expect(screen.getByText("New mail was waiting on sync at the last check")).toBeInTheDocument();
        expect(screen.getByText(/Last observed backlog: 5 messages/)).toBeInTheDocument();
        fixtures.data!.state!.deferred = 0;
        fixtures.data!.state!.last_synced_at = new Date();
        view.rerender(<SyncStatusCard mailboxId="mailbox" />);
        expect(screen.getByText("Last successful sync just now.")).toBeInTheDocument();
        expect(screen.queryByText(/Last observed backlog/)).not.toBeInTheDocument();
    });

    it("describes import progress separately from the last successful live check", () => {
        fixtures.data!.state!.backfill_status = "running";
        draw();
        expect(screen.getByText("Importing recent mail: 42 messages so far")).toBeInTheDocument();
        expect(screen.getByText("No successful sync check reported yet.")).toBeInTheDocument();
    });
});
