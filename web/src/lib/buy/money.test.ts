import { describe, expect, it } from "vitest";
import { cardLabel, fmtMicros, fmtMicrosShort, fmtMonthly, fmtYearly, microsToCents } from "./money";

describe("store money", () => {
    it("formats micro-USD as dollars and cents", () => {
        expect(fmtMicros(14_170_000)).toBe("$14.17");
        expect(fmtMicros(0)).toBe("$0.00");
        expect(fmtMicros(1_234_560_000)).toBe("$1,234.56");
    });

    it("rounds to the nearest cent, half up", () => {
        expect(microsToCents(11_174_999)).toBe(1117);
        expect(microsToCents(11_175_000)).toBe(1118);
    });

    it("drops the cents on whole dollars in short labels", () => {
        expect(fmtMicrosShort(3_000_000)).toBe("$3");
        expect(fmtMicrosShort(3_500_000)).toBe("$3.50");
        expect(fmtMonthly(9_000_000)).toBe("$9/mo");
        expect(fmtYearly(11_170_000)).toBe("$11.17/yr");
    });

    it("names the saved card", () => {
        expect(cardLabel({ brand: "visa", last4: "4242" })).toBe("Visa •••• 4242");
        expect(cardLabel({ brand: "amex", last4: "0005" })).toBe("American Express •••• 0005");
        expect(cardLabel({ brand: "", last4: "1111" })).toBe("Card •••• 1111");
    });
});
