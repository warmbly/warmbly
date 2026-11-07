import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import WarmupDaysPicker from "./WarmupDaysPicker";

function Picker() {
    const [days, setDays] = useState(0);
    return <WarmupDaysPicker value={days} onChange={setDays} inheritedDays={31} />;
}

describe("warmup day selection", () => {
    it("shows inherited days and lets the customer add weekends without changing campaign days", () => {
        render(<Picker />);
        expect(screen.getByText("Following sending plan: Monday to Friday.")).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Monday" })).toHaveAttribute("aria-pressed", "true");
        expect(screen.getByRole("button", { name: "Saturday" })).toHaveAttribute("aria-pressed", "false");
        fireEvent.click(screen.getByRole("button", { name: "Saturday" }));
        expect(screen.getByRole("button", { name: "Saturday" })).toHaveAttribute("aria-pressed", "true");
        expect(screen.getByRole("button", { name: "Follow sending plan" })).toHaveAttribute("aria-pressed", "false");
        expect(screen.getByText(/Campaign working days stay separate/)).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Follow sending plan" }));
        expect(screen.getByRole("button", { name: "Saturday" })).toHaveAttribute("aria-pressed", "false");
    });

    it("selects every day explicitly and keeps at least one selected day", () => {
        render(<Picker />);
        fireEvent.click(screen.getByRole("button", { name: "Every day" }));
        for (const day of ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"]) {
            expect(screen.getByRole("button", { name: day })).toHaveAttribute("aria-pressed", "true");
            fireEvent.click(screen.getByRole("button", { name: day }));
        }
        expect(screen.getByRole("button", { name: "Sunday" })).toHaveAttribute("aria-pressed", "true");
        expect(screen.getByRole("button", { name: "Follow sending plan" })).toHaveAttribute("aria-pressed", "false");
    });
});
