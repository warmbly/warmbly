import { beforeEach, describe, expect, it, vi } from "vitest";
import getSubscription from "./getSubscription";
import Request from "../../Request";

vi.mock("../../Request", () => ({ default: vi.fn() }));

beforeEach(() => { vi.mocked(Request).mockReset(); });

describe("getSubscription", () => {
    it("reads a workspace with no subscription as null rather than a failed load", async () => {
        vi.mocked(Request).mockRejectedValue({ error: "Not Found", message: "no subscription found", status: 404 });
        await expect(getSubscription()).resolves.toBeNull();
    });
    it("still fails on any other error", async () => {
        vi.mocked(Request).mockRejectedValue({ error: "Internal", message: "failed", status: 500 });
        await expect(getSubscription()).rejects.toMatchObject({ status: 500 });
    });
});
