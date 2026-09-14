import { describe, expect, it } from "vitest";

import * as sel from "./rowSelection";
import type { RowSelection } from "./rowSelection";

// One group of a larger, server-paged list: the tasks in one due-date bucket.
const bucket = ["a", "b"];
const rest = ["c", "d"];
const TOTAL = 400;

describe("a checkbox over one group of rows", () => {
    it("ticks the group without touching the rest", () => {
        let s = sel.toggleRow(sel.emptySelection, "c", true);
        s = sel.toggleGroup(s, bucket);
        expect(sel.selectionCount(s, TOTAL)).toBe(3);
        for (const id of [...bucket, "c"]) expect(sel.isRowSelected(s, id)).toBe(true);
        expect(sel.isRowSelected(s, "d")).toBe(false);
    });

    it("unticks only its own rows", () => {
        let s = sel.toggleGroup(sel.emptySelection, [...bucket, ...rest]);
        s = sel.toggleGroup(s, bucket);
        expect(sel.selectionCount(s, TOTAL)).toBe(2);
        expect(sel.isRowSelected(s, "a")).toBe(false);
        expect(sel.isRowSelected(s, "c")).toBe(true);
    });

    // The bug this guards: routing a group header through toggleLoaded, which
    // reads an all-ticked box as "clear everything", threw away a select-all of
    // thousands because one group of two was unticked.
    it("excludes the group from a select-all instead of dropping it", () => {
        let s = sel.selectAllMatching();
        s = sel.toggleGroup(s, bucket);
        expect(s.all).toBe(true);
        expect(sel.selectionCount(s, TOTAL)).toBe(TOTAL - bucket.length);
        expect(sel.isRowSelected(s, "a")).toBe(false);
        expect(sel.isRowSelected(s, "c")).toBe(true);
    });

    it("puts an excluded group back", () => {
        let s = sel.toggleGroup(sel.selectAllMatching(), bucket);
        s = sel.toggleGroup(s, bucket);
        expect(sel.selectionCount(s, TOTAL)).toBe(TOTAL);
        expect(s.excluded).toEqual([]);
    });

    it("does not double-count a group ticked twice over", () => {
        let s = sel.toggleRow(sel.emptySelection, "a", true);
        s = sel.toggleGroup(s, bucket);
        expect(s.ids).toEqual(["a", "b"]);
    });
});

// toggleLoaded keeps its own meaning: it is the header over EVERY loaded row,
// where an all-ticked box means "clear the selection".
describe("the header checkbox over every loaded row", () => {
    it("clears a select-all rather than excluding the page", () => {
        const s = sel.toggleLoaded(sel.selectAllMatching(), bucket);
        expect(s).toEqual(sel.emptySelection);
    });
});

describe("pruneExcluded", () => {
    it("drops exclusions the search no longer returns", () => {
        const s: RowSelection = { all: true, ids: [], excluded: ["a", "b"] };
        expect(sel.pruneExcluded(s, ["b", "c"])).toEqual({ all: true, ids: [], excluded: ["b"] });
    });

    it("returns the same object when every exclusion is still there", () => {
        const s: RowSelection = { all: true, ids: [], excluded: ["a"] };
        expect(sel.pruneExcluded(s, ["a", "b"])).toBe(s);
    });

    it("leaves an id-list selection alone", () => {
        const s: RowSelection = { all: false, ids: ["a"], excluded: [] };
        expect(sel.pruneExcluded(s, [])).toBe(s);
    });

    it("keeps the selection usable when the rows it excluded are all gone", () => {
        const s: RowSelection = { all: true, ids: [], excluded: ["a", "b", "c"] };
        const pruned = sel.pruneExcluded(s, ["d", "e"]);
        expect(sel.selectionCount(pruned, 2)).toBe(2);
    });
});
