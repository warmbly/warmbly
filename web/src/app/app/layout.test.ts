import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// The global modals are siblings of AppLayout, not children of it, and they sit
// lower in the file than the provider that has to reach them. That makes it
// easy to close a provider above them and not notice: nothing fails to compile,
// nothing fails to render, and the throw only arrives when someone opens the
// one modal that uses the context.
//
// MailboxAllowanceDialog, reached through AddEmailModal, calls useUpgradeDialog
// to offer the plan that lifts the mailbox cap. With it outside the provider
// that threw "UpgradeDialogProvider not found" at the exact moment a customer
// hit their allowance.
const source = readFileSync(join(__dirname, "layout.tsx"), "utf8");

const GLOBAL_MODALS = [
    "<TagsModal />",
    "<FoldersModal />",
    "<AddEmailModal />",
    "<ComposeWindow />",
    "<PasskeyEnrollPrompt />",
    "<PermissionDeniedModal />",
];

function between(open: string, close: string): string {
    const start = source.indexOf(open);
    const end = source.indexOf(close);
    expect(start, `${open} missing from layout.tsx`).toBeGreaterThan(-1);
    expect(end, `${close} missing from layout.tsx`).toBeGreaterThan(start);
    return source.slice(start, end);
}

describe("the /app provider tree", () => {
    it("keeps every global modal inside UpgradeDialogProvider", () => {
        const inside = between("<UpgradeDialogProvider>", "</UpgradeDialogProvider>");
        for (const modal of GLOBAL_MODALS) {
            expect(inside, `${modal} is mounted outside UpgradeDialogProvider`).toContain(modal);
        }
    });

    it("keeps every global modal inside ConfirmProvider", () => {
        // Their action handlers call useConfirm(); this is the same trap.
        const inside = between("<ConfirmProvider>", "</ConfirmProvider>");
        for (const modal of GLOBAL_MODALS) {
            expect(inside, `${modal} is mounted outside ConfirmProvider`).toContain(modal);
        }
    });
});
