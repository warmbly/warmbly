// Issue #469: with "Fixed schedule" selected, the whole profile below the
// toggle still looked and behaved like a live form, so the panel read as if
// those hours and volumes were in force when the mailbox was on its fixed cap
// and minimum gap. It is a disabled <fieldset> now.
//
// This mounts the real tab rather than asserting on class names, because the
// property that matters is the native one: a disabled fieldset has to actually
// disable its descendants, including the weekday buttons and the number
// steppers, which take no `disabled` prop of their own.

import React from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DEFAULT_SENDING_BEHAVIOR } from "@/lib/api/models/app/emails/SendingBehavior";

vi.mock("@/lib/api/client/Request", () => ({
    default: (cfg: { url?: string }) => {
        if (cfg?.url?.endsWith("/behavior")) {
            return Promise.resolve({ ...DEFAULT_SENDING_BEHAVIOR, timezone: "UTC" });
        }
        // The plan is only fetched for an enabled profile; leave it pending.
        return new Promise(() => {});
    },
}));
vi.mock("react-hot-toast", () => ({ default: { success: vi.fn(), error: vi.fn() } }));

import SendingBehaviorTab from "./SendingBehaviorTab";

function mount() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
        <QueryClientProvider client={client}>
            <SendingBehaviorTab mailboxId="mb-1" timezone="UTC" />
        </QueryClientProvider>,
    );
}

// The profile toggle is the first switch on the panel; the lunch one sits
// inside the fieldset and is therefore disabled in the off state.
const profileToggle = () => screen.getAllByRole("switch")[0];

describe("SendingBehaviorTab on the fixed schedule", () => {
    it("disables the whole profile and says why", async () => {
        mount();
        await screen.findByText("Fixed schedule");

        expect(screen.getByText(/nothing below applies/i)).toBeTruthy();
        // A plain button (weekday cell) and a spinbutton (a range bound): both
        // inherit their disabled state from the fieldset alone.
        expect(screen.getByTitle("Mon")).toBeDisabled();
        expect(screen.getAllByRole("spinbutton")[0]).toBeDisabled();
        // The profile switch itself must stay live, or the panel is a dead end.
        expect(profileToggle()).not.toBeDisabled();
    });

    it("releases everything when the profile is switched on", async () => {
        mount();
        await screen.findByText("Fixed schedule");

        fireEvent.click(profileToggle());

        await waitFor(() => expect(screen.getByText("Sending like a person")).toBeTruthy());
        expect(screen.queryByText(/nothing below applies/i)).toBeNull();
        expect(screen.getByTitle("Mon")).not.toBeDisabled();
        expect(screen.getAllByRole("spinbutton")[0]).not.toBeDisabled();
    });

    it("says the delay range replaces the mailbox minimum gap", async () => {
        mount();
        await screen.findByText("Fixed schedule");
        expect(screen.getByText(/replaces the mailbox's minimum gap/i)).toBeTruthy();
    });
});
