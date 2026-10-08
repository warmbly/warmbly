import { fireEvent, render, screen } from "@testing-library/react";
import { type ComponentProps, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import CategoryPicker from "./CategoryPicker";

vi.mock("@/lib/api/hooks/app/categories/useCreateCategory", () => ({ default: () => ({ isPending: false }) }));

function Browse({ showMissing = true }: { showMissing?: boolean }) {
    const [labels, setLabels] = useState(["missing"]);
    const value = { user: { id: "member", categories: [] } } as unknown as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>
        <CategoryPicker showMissing={showMissing} value={labels} onChange={setLabels} allowCreate={false} />
        <output>{labels.join(",")}</output>
    </UserContext.Provider>;
}

describe("unavailable label metadata", () => {
    it("keeps browse labels visibly removable while labels are unavailable", () => {
        render(<Browse />);
        expect(screen.getByText("Selected label")).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Remove Selected label" }));
        expect(screen.getByRole("status")).toHaveTextContent("");
        expect(screen.queryByText("Selected label")).not.toBeInTheDocument();
    });
    it("does not change editor picker behavior unless opted in", () => {
        render(<Browse showMissing={false} />);
        expect(screen.queryByText("Selected label")).not.toBeInTheDocument();
        expect(screen.getByRole("status")).toHaveTextContent("missing");
    });
});
