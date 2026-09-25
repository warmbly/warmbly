// Issue #415: a contact who unsubscribed was re-subscribed by lifting their
// suppression on the panel's Overview tab. That is a server-side change to the
// same record the Details tab is holding a draft of, so the draft and the
// record disagreed about `subscribed` and closing the panel asked to discard
// changes the user never made.

import { describe, it, expect } from "vitest";
import {
    fieldsOf,
    hasUnnamedValue,
    idsOf,
    rebase,
    rebaseFields,
    recordFromCF,
    sameCampaigns,
    sameFields,
    sameIDs,
    sameRecords,
    sameRows,
} from "./rebase";

describe("rebase", () => {
    it("adopts the server's new value for a field the user did not touch", () => {
        // The suppression lift re-subscribed them: false -> true, with the
        // draft still sitting on the old value it started from.
        expect(rebase(false, false, true)).toBe(true);
    });

    it("keeps the user's edit when the server moved too", () => {
        expect(rebase("Testing", "Test", "Tested")).toBe("Testing");
    });

    it("keeps the user's edit when they already made the same change", () => {
        expect(rebase(true, false, true)).toBe(true);
    });

    it("is a no-op when nothing moved", () => {
        expect(rebase("Test", "Test", "Test")).toBe("Test");
    });

    it("takes a comparison for values that are not primitives", () => {
        const prev = [{ id: "a", name: "Agency" }];
        const next = [{ id: "a", name: "Agency" }, { id: "b", name: "SaaS" }];
        // Same ids in a different array: still untouched, so adopt.
        expect(rebase([{ id: "a", name: "Agency" }], prev, next, sameCampaigns)).toBe(next);
        // The user removed one: keep their draft.
        const local: typeof prev = [];
        expect(rebase(local, prev, next, sameCampaigns)).toBe(local);
    });
});

describe("sameIDs", () => {
    it("ignores order", () => {
        expect(sameIDs(["a", "b"], ["b", "a"])).toBe(true);
    });

    it("notices an addition and a removal", () => {
        expect(sameIDs(["a"], ["a", "b"])).toBe(false);
        expect(sameIDs(["a", "b"], ["a"])).toBe(false);
    });

    it("treats two empty sets as equal", () => {
        expect(sameIDs([], [])).toBe(true);
    });

    it("is not fooled by a repeated id", () => {
        expect(sameIDs(["a", "a"], ["a", "b"])).toBe(false);
    });
});

describe("idsOf", () => {
    it("pulls ids out in order", () => {
        expect(idsOf([{ id: "a" }, { id: "b" }])).toEqual(["a", "b"]);
    });
});

describe("custom fields", () => {
    it("round-trips a record through the row form", () => {
        const record = { industry: "Freight", "job title": "Ops lead" };
        expect(recordFromCF(fieldsOf(record))).toEqual(record);
    });

    it("drops unnamed rows and trims names", () => {
        expect(
            recordFromCF([
                { name: "  industry ", value: "Freight" },
                { name: "   ", value: "orphaned" },
            ]),
        ).toEqual({ industry: "Freight" });
    });

    // An empty value removes the key on save, so it is no field at all.
    it("drops empty values and collapses spaces in names", () => {
        expect(
            recordFromCF([
                { name: "job   title", value: "Ops lead" },
                { name: "tier", value: "" },
                { name: "notes", value: "  " },
            ]),
        ).toEqual({ "job title": "Ops lead", notes: "  " });
    });

    it("compares rows as the record they save as", () => {
        expect(
            sameFields(
                [{ name: "industry", value: "Freight" }, { name: "", value: "" }],
                [{ name: "industry", value: "Freight" }],
            ),
        ).toBe(true);
        expect(
            sameFields(
                [{ name: "industry", value: "Freight" }],
                [{ name: "industry", value: "Rail" }],
            ),
        ).toBe(false);
    });

    it("survives an undefined record", () => {
        expect(fieldsOf(undefined)).toEqual([]);
    });
});

describe("sameRecords", () => {
    it("ignores key order", () => {
        expect(sameRecords({ a: "1", b: "2" }, { b: "2", a: "1" })).toBe(true);
    });

    it("notices a changed value, an extra key and a missing one", () => {
        expect(sameRecords({ a: "1" }, { a: "2" })).toBe(false);
        expect(sameRecords({ a: "1" }, { a: "1", b: "2" })).toBe(false);
        expect(sameRecords({ a: "1", b: "2" }, { a: "1" })).toBe(false);
    });

    it("does not mistake a key for one on the prototype", () => {
        expect(sameRecords({ toString: "x" }, {} as Record<string, string>)).toBe(false);
    });
});

describe("sameFields", () => {
    // Removing a custom-field row and typing it back leaves the same record
    // with its keys in a different order. Comparing the two as JSON said they
    // differed, which left the panel permanently unsaved.
    it("ignores the order the rows were entered in", () => {
        expect(
            sameFields(
                [
                    { name: "industry", value: "Freight" },
                    { name: "tier", value: "A" },
                ],
                [
                    { name: "tier", value: "A" },
                    { name: "industry", value: "Freight" },
                ],
            ),
        ).toBe(true);
    });
});

describe("sameRows", () => {
    // A row the user has typed a value into but not yet named saves as
    // nothing, so `sameFields` calls it untouched. The rebase must not, or
    // adopting a server change deletes what they were typing.
    it("sees a half-typed row that sameFields does not", () => {
        const draft = [
            { name: "industry", value: "Freight" },
            { name: "", value: "half typed" },
        ];
        const server = [{ name: "industry", value: "Freight" }];
        expect(sameFields(draft, server)).toBe(true);
        expect(sameRows(draft, server)).toBe(false);
        expect(rebase(draft, server, [{ name: "industry", value: "Rail" }], sameRows)).toBe(draft);
    });

    it("adopts the server rows when the draft is untouched", () => {
        const server = [{ name: "industry", value: "Freight" }];
        const next = [{ name: "industry", value: "Rail" }];
        expect(rebase([{ name: "industry", value: "Freight" }], server, next, sameRows)).toBe(next);
    });
});

describe("recordFromCF", () => {
    // Assigning to `__proto__` sets the prototype instead of adding an own
    // property, so a field by that name vanished from what was saved.
    it("keeps a field named __proto__", () => {
        const record = recordFromCF([{ name: "__proto__", value: "Freight" }]);
        expect(Object.hasOwn(record, "__proto__")).toBe(true);
        expect(record["__proto__"]).toBe("Freight");
        expect(Object.getPrototypeOf(record)).toBe(Object.prototype);
    });

    it("notices a change to a field named __proto__", () => {
        expect(
            sameFields([{ name: "__proto__", value: "a" }], [{ name: "__proto__", value: "b" }]),
        ).toBe(false);
    });
});

describe("hasUnnamedValue", () => {
    it("sees a row typed into but not named", () => {
        expect(hasUnnamedValue([{ name: "", value: "half typed" }])).toBe(true);
        expect(hasUnnamedValue([{ name: "   ", value: "half typed" }])).toBe(true);
    });

    it("ignores an empty row and a named one", () => {
        expect(hasUnnamedValue([{ name: "", value: "" }])).toBe(false);
        expect(hasUnnamedValue([{ name: "", value: "   " }])).toBe(false);
        expect(hasUnnamedValue([{ name: "industry", value: "Freight" }])).toBe(false);
        expect(hasUnnamedValue([])).toBe(false);
    });
});

describe("rebaseFields", () => {
    // A draft that happens to match a stored row is still the user's work.
    it("does not mistake a draft for the stored row it matches", () => {
        const draft = [{ name: "tier", value: "A", draft: true }];
        expect(sameRows(draft, [{ name: "tier", value: "A" }])).toBe(false);
    });

    const prev = [
        { name: "industry", value: "Freight" },
        { name: "tier", value: "A" },
    ];

    // The save is a diff against the server, so a field the user never saw
    // has to reach the draft or the diff deletes it.
    it("takes a field a teammate added while the user edited another", () => {
        const local = [
            { name: "industry", value: "Freight" },
            { name: "tier", value: "B" },
        ];
        const next = [...prev, { name: "region", value: "EU" }];
        expect(rebaseFields(local, prev, next)).toEqual([
            { name: "industry", value: "Freight" },
            { name: "tier", value: "B" },
            { name: "region", value: "EU" },
        ]);
    });

    it("takes the server's value for a field the user left alone", () => {
        const local = [
            { name: "tier", value: "A" },
            { name: "industry", value: "Freight" },
            { name: "", value: "half typed", draft: true },
        ];
        const next = [
            { name: "industry", value: "Rail" },
            { name: "tier", value: "A" },
        ];
        expect(rebaseFields(local, prev, next)).toEqual([
            { name: "tier", value: "A" },
            { name: "industry", value: "Rail" },
            { name: "", value: "half typed", draft: true },
        ]);
    });

    it("keeps the user's removal and edit over the server's change", () => {
        const local = [{ name: "tier", value: "C" }];
        const next = [
            { name: "industry", value: "Rail" },
            { name: "tier", value: "B" },
        ];
        expect(rebaseFields(local, prev, next)).toEqual([{ name: "tier", value: "C" }]);
    });

    it("drops a field the server removed that the user left alone", () => {
        const local = [
            { name: "industry", value: "Freight" },
            { name: "tier", value: "A" },
            { name: "", value: "x", draft: true },
        ];
        expect(rebaseFields(local, prev, [{ name: "industry", value: "Freight" }])).toEqual([
            { name: "industry", value: "Freight" },
            { name: "", value: "x", draft: true },
        ]);
    });
});
