// Issue #680: a contact form offers every field the workspace already has as
// its own input, so filling one in is typing a value, not retyping its name.

import React from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { CustomField } from "./customFields";

const workspaceKeys = vi.hoisted(() => ({ current: [] as string[] }));
vi.mock("@/lib/api/hooks/app/contacts/useCustomFieldKeys", () => ({
    default: () => ({ data: workspaceKeys.current, isPending: false }),
}));

const CustomFieldsEditor = (await import("./CustomFieldsEditor")).default;

let latest: CustomField[] = [];

function Harness({ initial = [] }: { initial?: CustomField[] }) {
    const [rows, setRows] = React.useState<CustomField[]>(initial);
    latest = rows;
    return <CustomFieldsEditor value={rows} onChange={setRows} />;
}

function input(label: string): HTMLInputElement {
    return screen.getByLabelText(label) as HTMLInputElement;
}

describe("CustomFieldsEditor", () => {
    it("shows each workspace field as an input and fills it by value alone", () => {
        workspaceKeys.current = ["industry", "tier"];
        render(<Harness />);

        fireEvent.change(input("tier"), { target: { value: "A" } });
        expect(latest).toEqual([{ name: "tier", value: "A" }]);
        expect(input("industry").value).toBe("");
    });

    it("keeps a cleared field on screen and drops its row", () => {
        workspaceKeys.current = [];
        render(<Harness initial={[{ name: "legacy", value: "x" }]} />);

        fireEvent.change(input("legacy"), { target: { value: "" } });
        expect(latest).toEqual([]);
        expect(input("legacy").value).toBe("");
    });

    it("hides empty fields past the first few behind Show more", () => {
        workspaceKeys.current = ["a", "b", "c", "d", "e", "f", "g", "h"];
        render(<Harness initial={[{ name: "h", value: "set" }]} />);

        expect(screen.queryByLabelText("g")).toBeNull();
        expect(input("h").value).toBe("set");
        fireEvent.click(screen.getByText("Show 1 more"));
        expect(screen.getByLabelText("g")).toBeTruthy();
    });

    it("moves a new field typed with an existing name onto that field", () => {
        workspaceKeys.current = ["industry"];
        render(<Harness />);

        fireEvent.click(screen.getByText("New field"));
        const [name, value] = screen.getAllByPlaceholderText(/Field name|Value/);
        fireEvent.change(value, { target: { value: "Freight" } });
        fireEvent.change(name, { target: { value: "Industry" } });
        expect(screen.getByText(/You already have/)).toBeTruthy();

        fireEvent.click(screen.getByText("Use it"));
        expect(latest).toEqual([{ name: "industry", value: "Freight" }]);
        expect(input("industry").value).toBe("Freight");
    });

    it("offers workspace fields while a new one is named", () => {
        workspaceKeys.current = ["job title", "industry"];
        render(<Harness />);

        fireEvent.click(screen.getByText("New field"));
        const name = screen.getByPlaceholderText("Field name");
        fireEvent.focus(name);
        fireEvent.change(name, { target: { value: "job" } });
        fireEvent.click(screen.getByRole("option", { name: "job title" }));

        // Nothing typed as a value yet, so the draft simply becomes the field.
        expect(latest).toEqual([]);
        expect(screen.queryByPlaceholderText("Field name")).toBeNull();
    });
});
