import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import WarmupFilterTagField from "./WarmupFilterTagField";

const state = vi.hoisted(() => ({ tag: null as string | null, mutateAsync: vi.fn(async () => ({ tag: "k7q2-mx94-ab3d" })) }));
vi.mock("@/lib/api/hooks/app/emails/useWarmupFilterTag", () => ({
    useWarmupFilterTag: () => ({ data: { tag: state.tag }, isLoading: false, isError: false }),
    useSetWarmupFilterTag: () => ({ mutateAsync: state.mutateAsync, isPending: false }),
}));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: (_: string, fn: () => void) => fn() }) }));

afterEach(() => {
    cleanup();
    state.mutateAsync.mockClear();
});

describe("WarmupFilterTagField", () => {
    it("is off by default and turning it on asks the server for a code", () => {
        state.tag = null;
        render(<WarmupFilterTagField mailboxId="m1" provider="gmail" />);
        expect(screen.queryByText(/k7q2/)).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("switch"));
        expect(state.mutateAsync).toHaveBeenCalledWith(undefined);
    });

    it("shows the code with filter steps for the mailbox's provider, and turns off with null", () => {
        state.tag = "k7q2-mx94-ab3d";
        render(<WarmupFilterTagField mailboxId="m1" provider="outlook" />);
        expect(screen.getByText("k7q2-mx94-ab3d")).toBeInTheDocument();
        expect(screen.getByText(/In Outlook, add a rule/)).toBeInTheDocument();
        fireEvent.click(screen.getByRole("switch"));
        expect(state.mutateAsync).toHaveBeenCalledWith(null);
    });

    it("issues a new code after confirming", () => {
        state.tag = "k7q2-mx94-ab3d";
        render(<WarmupFilterTagField mailboxId="m1" provider="smtp_imap" />);
        fireEvent.click(screen.getByRole("button", { name: "New code" }));
        expect(state.mutateAsync).toHaveBeenCalledWith(undefined);
    });
});
