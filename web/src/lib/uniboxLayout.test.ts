import { describe, it, expect } from "vitest";
import {
    UNIBOX_CONTACT_RAIL_WIDTH,
    UNIBOX_THREAD_MIN_WIDTH,
    uniboxListMaxWidth,
    uniboxThreadReserve,
} from "./uniboxLayout";
import {
    UNIBOX_LIST_MAX_WIDTH,
    UNIBOX_LIST_MIN_WIDTH,
} from "@/stores/slices/uiSlice";

// The app nav is 256px and the content panel has a 1px left border, so the
// unibox row starts this far in.
const rowLeft = (viewport: number) => 257;
const row = (viewport: number) => ({ left: rowLeft(viewport), right: viewport });
// The scope rail is 220px and only renders from lg.
const listLeft = (viewport: number, wide: boolean) =>
    rowLeft(viewport) + (wide ? 220 : 0);

describe("uniboxListMaxWidth", () => {
    it("leaves the thread its minimum on a laptop", () => {
        // 1512 - 257 - 220 = 1035 of row after the rail, minus 6 for the handle
        // and 360 for the thread.
        expect(
            uniboxListMaxWidth({
                rowRight: row(1512).right,
                listLeft: listLeft(1512, true),
                reservedForThread: uniboxThreadReserve(false),
            }),
        ).toBe(UNIBOX_LIST_MAX_WIDTH);
    });

    it("counts the contact rail, which lives inside the thread pane", () => {
        const geometry = {
            rowRight: row(1440).right,
            listLeft: listLeft(1440, true),
        };
        const withRail = uniboxListMaxWidth({
            ...geometry,
            reservedForThread: uniboxThreadReserve(true),
        });
        const withoutRail = uniboxListMaxWidth({
            ...geometry,
            reservedForThread: uniboxThreadReserve(false),
        });
        expect(withRail).toBeLessThan(withoutRail);

        // The invariant that matters: at the cap, with the rail open, there is
        // still a readable message column. Reserving only the thread pane left
        // ~34px of message text on this exact viewport, because the 320px
        // contact rail is a sibling INSIDE it.
        const messageColumn = (cap: number) =>
            geometry.rowRight - geometry.listLeft - 6 - cap - UNIBOX_CONTACT_RAIL_WIDTH;
        expect(messageColumn(withRail)).toBeGreaterThanOrEqual(
            UNIBOX_THREAD_MIN_WIDTH - 4,
        );
        expect(messageColumn(withoutRail)).toBeLessThan(100);
    });

    it("never caps below the list minimum, so the handle always moves something", () => {
        // A tablet at 1024: 1024 - 257 - 220 = 547 of row, which cannot hold a
        // 280px list AND a 360px thread. The thread gives way, exactly as it
        // did when the list was a fixed 360px column.
        for (const viewport of [768, 820, 1024, 1116]) {
            const wide = viewport >= 1024;
            expect(
                uniboxListMaxWidth({
                    rowRight: row(viewport).right,
                    listLeft: listLeft(viewport, wide),
                    reservedForThread: uniboxThreadReserve(false),
                }),
            ).toBeGreaterThanOrEqual(UNIBOX_LIST_MIN_WIDTH);
        }
    });

    it("declines to cap when the element has no layout", () => {
        // jsdom, or a display:none ancestor: every rect is zero. Capping on
        // that would pin the column to its minimum for no reason.
        expect(
            uniboxListMaxWidth({ rowRight: 0, listLeft: 0, reservedForThread: 360 }),
        ).toBe(UNIBOX_LIST_MAX_WIDTH);
        expect(
            uniboxListMaxWidth({ rowRight: NaN, listLeft: 0, reservedForThread: 360 }),
        ).toBe(UNIBOX_LIST_MAX_WIDTH);
    });
});

describe("uniboxThreadReserve", () => {
    it("is the message column alone when the contact rail is hidden", () => {
        expect(uniboxThreadReserve(false)).toBe(UNIBOX_THREAD_MIN_WIDTH);
        expect(uniboxThreadReserve(true)).toBe(
            UNIBOX_THREAD_MIN_WIDTH + UNIBOX_CONTACT_RAIL_WIDTH,
        );
    });
});
