import { describe, it, expect } from "vitest";
import {
    customFieldsPatch,
    customFieldsProblem,
    draftMatch,
    labelledKeys,
    suggestKeys,
} from "./customFields";

describe("labelledKeys", () => {
    it("keeps the workspace order and appends fields the list has not caught up with", () => {
        expect(labelledKeys(["industry", "tier"], ["tier", "account owner", ""])).toEqual([
            "industry",
            "tier",
            "account owner",
        ]);
    });
});

describe("draftMatch", () => {
    it("finds the exact field and one spelled differently", () => {
        expect(draftMatch(" industry ", ["industry"])).toEqual({ key: "industry", exact: true });
        expect(draftMatch("Job-Title", ["job title"])).toEqual({ key: "job title", exact: false });
        expect(draftMatch("region", ["industry"])).toBeNull();
    });
});

describe("suggestKeys", () => {
    it("matches ignoring case and separators, most used first", () => {
        expect(suggestKeys("jobti", ["industry", "Job Title", "job_title_2"])).toEqual([
            "Job Title",
            "job_title_2",
        ]);
        expect(suggestKeys("", ["a", "b", "c"], 2)).toEqual(["a", "b"]);
    });
});

describe("customFieldsProblem", () => {
    it("accepts labelled fields, blank drafts and valid new names", () => {
        expect(
            customFieldsProblem([
                { name: "industry", value: "Freight" },
                { name: "", value: "", draft: true },
                { name: "Company Mobile", value: "+1", draft: true },
            ]),
        ).toBeNull();
    });

    it("refuses a filled row with no name", () => {
        expect(customFieldsProblem([{ name: " ", value: "x", draft: true }])).toMatch(/Name every/);
    });

    it("refuses a name the API cannot store", () => {
        expect(customFieldsProblem([{ name: "a/b", value: "x", draft: true }])).toMatch(/cannot be used/);
    });

    it("refuses the same field filled in twice", () => {
        expect(
            customFieldsProblem([
                { name: "industry", value: "Freight" },
                { name: "industry ", value: "Rail", draft: true },
            ]),
        ).toMatch(/filled in twice/);
    });

    it("lets a draft name a field that is still empty", () => {
        expect(
            customFieldsProblem([
                { name: "industry", value: "" },
                { name: "industry", value: "Rail", draft: true },
            ]),
        ).toBeNull();
    });
});

describe("customFieldsPatch", () => {
    // The server merges the patch into what it stores, so a key left out is
    // kept; removing one has to send it empty.
    it("sends a removed or cleared field as empty", () => {
        expect(
            customFieldsPatch({ industry: "Freight", tier: "A" }, [{ name: "tier", value: "   " }]),
        ).toEqual({ industry: "", tier: "" });
    });

    it("sends only what changed", () => {
        expect(
            customFieldsPatch({ industry: "Freight", tier: "A" }, [
                { name: "industry", value: "Freight" },
                { name: "tier", value: "B" },
                { name: "region", value: "EU", draft: true },
            ]),
        ).toEqual({ tier: "B", region: "EU" });
    });

    it("sends nothing for an untouched record", () => {
        expect(customFieldsPatch({ industry: "Freight" }, [{ name: "industry", value: "Freight" }])).toEqual({});
        expect(customFieldsPatch(undefined, [])).toEqual({});
    });
});
