import { describe, expect, it } from "vitest";

import type { FormField, FormFieldType } from "@/lib/api/models/app/forms/Form";
import {
    CANVAS_END_DROPPABLE_ID,
    PALETTE_PREFIX,
    insertionIndex,
    isNoopMove,
    moveField,
    rowStarts,
} from "./dropSlot";

const f = (id: string, width?: string, type: FormFieldType = "text"): FormField => ({
    id,
    type,
    label: id,
    required: false,
    ...(width ? { width } : {}),
});

const ids = (fields: FormField[]) => fields.map((x) => x.id).join(",");
const list = [f("a"), f("b"), f("c"), f("d")];

describe("insertionIndex", () => {
    it("is nothing when the release resolved to no target", () => {
        expect(insertionIndex(list, "a", null)).toBeNull();
        expect(insertionIndex(list, "a", "gone")).toBeNull();
    });

    it("puts a field dragged onto one below it after that one", () => {
        expect(insertionIndex(list, "a", "d")).toBe(4);
        expect(insertionIndex(list, "a", "b")).toBe(2);
    });

    it("puts a field dragged onto one above it before that one", () => {
        expect(insertionIndex(list, "d", "a")).toBe(0);
        expect(insertionIndex(list, "d", "c")).toBe(2);
    });

    it("sends the end zone to the last slot", () => {
        expect(insertionIndex(list, "a", CANVAS_END_DROPPABLE_ID)).toBe(4);
        expect(insertionIndex([], `${PALETTE_PREFIX}text`, CANVAS_END_DROPPABLE_ID)).toBe(0);
    });

    it("inserts a palette block before the field it was dropped on", () => {
        expect(insertionIndex(list, `${PALETTE_PREFIX}email`, "a")).toBe(0);
        expect(insertionIndex(list, `${PALETTE_PREFIX}email`, "d")).toBe(3);
    });
});

describe("isNoopMove", () => {
    it("holds for the two slots that leave a field where it is", () => {
        expect(isNoopMove(list, "b", 1)).toBe(true);
        expect(isNoopMove(list, "b", 2)).toBe(true);
        expect(isNoopMove(list, "b", 3)).toBe(false);
        expect(isNoopMove(list, "b", null)).toBe(true);
    });

    it("never holds for a palette block, which always adds one", () => {
        expect(isNoopMove(list, `${PALETTE_PREFIX}text`, 2)).toBe(false);
    });
});

describe("moveField", () => {
    it("drops the first field at the bottom", () => {
        expect(ids(moveField(list, "a", insertionIndex(list, "a", "d")!))).toBe("b,c,d,a");
        expect(ids(moveField(list, "a", insertionIndex(list, "a", CANVAS_END_DROPPABLE_ID)!))).toBe("b,c,d,a");
    });

    it("lifts the last field to the top", () => {
        expect(ids(moveField(list, "d", insertionIndex(list, "d", "a")!))).toBe("d,a,b,c");
    });

    it("moves one step either way", () => {
        expect(ids(moveField(list, "b", insertionIndex(list, "b", "c")!))).toBe("a,c,b,d");
        expect(ids(moveField(list, "c", insertionIndex(list, "c", "b")!))).toBe("a,c,b,d");
    });

    it("returns the same list when nothing moves", () => {
        expect(moveField(list, "b", 1)).toBe(list);
        expect(moveField(list, "b", 2)).toBe(list);
        expect(moveField(list, "gone", 0)).toBe(list);
    });

    it("leaves every other field in order", () => {
        const long = [f("a"), f("b"), f("c"), f("d"), f("e")];
        expect(ids(moveField(long, "e", 1))).toBe("a,e,b,c,d");
        expect(ids(moveField(long, "a", 3))).toBe("b,c,a,d,e");
    });
});

describe("rowStarts", () => {
    it("gives every full-width field its own row", () => {
        expect(rowStarts([f("a"), f("b")])).toEqual([true, true]);
    });

    it("pairs consecutive half-width fields into one row", () => {
        expect(rowStarts([f("a", "half"), f("b", "half"), f("c", "half")])).toEqual([true, false, true]);
    });

    it("breaks a pair with a full-width field between them", () => {
        expect(rowStarts([f("a", "half"), f("b"), f("c", "half"), f("d", "half")])).toEqual([true, true, true, false]);
    });

    it("ignores width on a page break, which always spans the row", () => {
        expect(rowStarts([f("a", "half"), f("b", "half", "page_break"), f("c", "half")])).toEqual([true, true, true]);
    });
});
