import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import type { PortalTheme, PublicBrand } from "@/lib/api/models/app/brand/Brand";
import PortalSignIn from "./PortalSignIn";

const brand = (theme: PortalTheme, extra: Partial<PublicBrand> = {}): PublicBrand => ({ name: "Northwind Growth", theme, ...extra });

const show = (b: PublicBrand) =>
    render(
        <PortalSignIn brand={b}>
            <form aria-label="Sign in" />
        </PortalSignIn>,
    );

describe("PortalSignIn", () => {
    it.each(["", "split", "cover"] as const)("draws the form and the brand in the %s layout, on every background", (layout) => {
        for (const background of ["", "accent", "dark", "image"] as const) {
            const { unmount } = show(brand({ sign_in_layout: layout, sign_in_background: background }));
            expect(screen.getByRole("form", { name: "Sign in" })).toBeInTheDocument();
            expect(screen.getAllByText(/Northwind Growth/).length).toBeGreaterThan(0);
            unmount();
        }
    });

    it("shows the agency's heading and message as text", () => {
        show(brand({ sign_in_layout: "split", sign_in_heading: "<b>Hello</b> there", sign_in_message: "Reports and replies in one place." }));
        expect(screen.getByRole("heading", { name: "<b>Hello</b> there" })).toBeInTheDocument();
        expect(screen.getByText("Reports and replies in one place.")).toBeInTheDocument();
    });

    it("writes a heading itself only where the layout has a brand panel", () => {
        const centred = show(brand({}));
        expect(screen.queryByRole("heading")).toBeNull();
        centred.unmount();
        show(brand({ sign_in_layout: "cover" }));
        expect(screen.getByRole("heading", { name: "Welcome to Northwind Growth" })).toBeInTheDocument();
    });

    it("links support, terms and privacy to the agency's own pages, and nothing when it set none", () => {
        const none = show(brand({}));
        expect(screen.queryByRole("link")).toBeNull();
        none.unmount();
        show(brand({ support_email: "help@northwind.test", terms_url: "https://northwind.test/terms", privacy_url: "https://northwind.test/privacy" }));
        expect(screen.getByRole("link", { name: "Support" })).toHaveAttribute("href", "mailto:help@northwind.test");
        expect(screen.getByRole("link", { name: "Terms" })).toHaveAttribute("href", "https://northwind.test/terms");
        expect(screen.getByRole("link", { name: "Privacy" })).toHaveAttribute("href", "https://northwind.test/privacy");
    });

    it("keeps the attribution the plan requires", () => {
        show(brand({}, { powered_by: { name: "Warmbly", url: "https://warmbly.test" } }));
        expect(screen.getByRole("link", { name: "Powered by Warmbly" })).toHaveAttribute("href", "https://warmbly.test");
    });

    it("draws the uploaded image as an image element, and the accent when there is none", () => {
        const withImage = show(brand({ sign_in_layout: "cover", sign_in_background: "image" }, { sign_in_image_url: "/brand/cover.jpg" }));
        const img = withImage.container.querySelector("img.object-cover");
        expect(img).toHaveAttribute("src", "/brand/cover.jpg");
        expect(withImage.container.innerHTML).not.toContain("url(");
        withImage.unmount();

        const without = show(brand({ sign_in_layout: "cover", sign_in_background: "image" }));
        expect(without.container.querySelector("img.object-cover")).toBeNull();
        expect(without.container.querySelector(".wb-signin-accent")).not.toBeNull();
    });

    it("ignores a theme of unknown choices and a link that is not https", () => {
        show(brand({ sign_in_layout: "grid" as never, sign_in_background: "url(x)" as never, support_url: "javascript:alert(1)" }));
        expect(screen.getByRole("form", { name: "Sign in" })).toBeInTheDocument();
        expect(screen.queryByRole("link")).toBeNull();
    });

    it("carries the theme on its own subtree when the page around it is not the brand's", () => {
        const { container } = render(
            <PortalSignIn brand={brand({ corners: "round" }, { accent_color: "#dc2626" })} scope="dark">
                <form aria-label="Sign in" />
            </PortalSignIn>,
        );
        const root = container.firstElementChild as HTMLElement;
        expect(root.classList.contains("dark")).toBe(true);
        expect(root.style.getPropertyValue("--radius")).toBe("0.75rem");
        expect(root.style.getPropertyValue("--color-sky-600")).toBe(root.style.getPropertyValue("--wb-sky-d-600"));
    });
});
