import { afterEach, describe, expect, it } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import toast from "react-hot-toast/headless";
import { Toaster } from "./toaster";

afterEach(() => toast.remove());

describe("static notification styling", () => {
    it.each(["blank", "success", "error", "loading"] as const)("uses theme-aware surface and text colors for %s notifications", (type) => {
        render(<Toaster />);
        act(() => {
            const notify = type === "blank" ? toast : toast[type];
            notify("This provider isn't available yet. OAuth credentials are not configured on the server.");
        });
        expect(screen.getByRole("status").parentElement).toHaveClass("bg-white", "text-slate-800");
        expect(screen.getByRole("status").parentElement).not.toHaveClass("text-[#363636]");
    });

    it("renders and dismisses errors without injecting a style element", () => {
        const before = document.querySelectorAll("style").length;
        render(<Toaster />);
        act(() => { toast.error("A detailed mailbox error"); });
        expect(screen.getByRole("status")).toHaveTextContent("A detailed mailbox error");
        expect(document.querySelectorAll("style")).toHaveLength(before);
        fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
        expect(document.querySelectorAll("style")).toHaveLength(before);
    });

    it("preserves loading, updates and custom notifications", () => {
        render(<Toaster />);
        let id = "";
        act(() => { id = toast.loading("Working"); });
        expect(screen.getByRole("status")).toHaveTextContent("Working");
        act(() => { toast.success("Saved", { id }); toast.custom(<span>Custom message</span>); });
        expect(screen.getByRole("status")).toHaveTextContent("Saved");
        expect(screen.getByText("Custom message")).toBeInTheDocument();
    });
});
