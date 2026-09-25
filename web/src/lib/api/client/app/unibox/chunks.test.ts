import { describe, expect, it } from "vitest";

import { inChunks, pairedChunks, UNIBOX_BULK_MAX } from "./chunks";

describe("unibox bulk batching", () => {
    it("keeps every request under the server's cap", async () => {
        const ids = Array.from({ length: 1234 }, (_, i) => `t${i}`);
        const sizes: number[] = [];
        await inChunks(ids, UNIBOX_BULK_MAX, async (chunk) => {
            sizes.push(chunk.length);
        });
        expect(sizes).toEqual([500, 500, 234]);
    });

    it("stops at the first failed batch", async () => {
        let sent = 0;
        await expect(
            inChunks(Array.from({ length: 1200 }), 500, async () => {
                sent++;
                if (sent === 2) throw new Error("boom");
            }),
        ).rejects.toThrow("boom");
        expect(sent).toBe(2);
    });

    it("slices two lists in step and still sends one request for none", () => {
        const threads = Array.from({ length: 612 }, (_, i) => `t${i}`);
        const parts = pairedChunks([], threads, UNIBOX_BULK_MAX);
        expect(parts.map((p) => [p.a.length, p.b.length])).toEqual([
            [0, 500],
            [0, 112],
        ]);
        expect(pairedChunks([], [], UNIBOX_BULK_MAX)).toEqual([{ a: [], b: [] }]);
    });
});
