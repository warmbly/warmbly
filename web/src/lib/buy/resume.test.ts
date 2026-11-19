import { describe, expect, it } from "vitest";
import { resumeKey, saveResume, settleResume, takeResume } from "./resume";

function memory(): Storage {
    const m = new Map<string, string>();
    return {
        get length() {
            return m.size;
        },
        clear: () => m.clear(),
        getItem: (k) => m.get(k) ?? null,
        key: (i) => [...m.keys()][i] ?? null,
        removeItem: (k) => void m.delete(k),
        setItem: (k, v) => void m.set(k, v),
    };
}

const body = { quote_id: "q1", expected_due_now_micros: 17_170_000 };

describe("checkout resume after card setup", () => {
    it("places the saved order once after a card is saved", () => {
        const s = memory();
        saveResume(body, 1_000, s);
        settleResume(true, s);
        expect(takeResume(2_000, s)).toEqual(body);
        expect(takeResume(3_000, s)).toBeNull();
        expect(resumeKey(body)).toBe("store-resume-q1");
    });

    it("drops the order when card setup was closed", () => {
        const s = memory();
        saveResume(body, 1_000, s);
        settleResume(false, s);
        expect(takeResume(2_000, s)).toBeNull();
    });

    it("never places an order that was not readied, or one left too long", () => {
        const s = memory();
        saveResume(body, 1_000, s);
        expect(takeResume(2_000, s)).toBeNull();
        saveResume(body, 0, s);
        settleResume(true, s);
        expect(takeResume(2 * 60 * 60 * 1000, s)).toBeNull();
    });
});
