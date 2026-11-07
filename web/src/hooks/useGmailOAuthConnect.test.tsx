import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import useGmailOAuthConnect from "./useGmailOAuthConnect";

const auth = vi.hoisted(() => ({ gmail_oauth_connect: false }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({
    default: () => ({ config: auth }),
}));

beforeEach(() => {
    auth.gmail_oauth_connect = false;
});

describe("Google mailbox sign-in availability", () => {
    it.each([
        { name: "shows when the backend allows it", backend: true, expected: true },
        { name: "hides when the backend disables it", backend: false, expected: false },
        { name: "shows for Cloud-brokered consent", viaCloud: true, backend: false, expected: true },
    ])("$name", ({ backend, viaCloud, expected }) => {
        auth.gmail_oauth_connect = backend;
        const { result, unmount } = renderHook(() => useGmailOAuthConnect(viaCloud));
        expect(result.current).toBe(expected);
        unmount();
    });
});
