import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Order } from "@/lib/api/models/app/store/Store";
import { STEP_KEYS } from "@/lib/buy/track";
import SetupTrack from "./SetupTrack";

const domain = (name: string, status: string) =>
    ({ id: name, name, status, provider: "google", expires_at: null, auto_renew: true, renew_micros: 0, forwarding_url: null, mail_auth: null, mailbox_count: 0, created_at: "", source: "purchased", renewable: false, forwarding_status: "none", nameservers: [], connection: null, dns_conflicts: [], owner_verification: null, transfer: null, tracking_host: "" }) as Order["domains"][number];

describe("SetupTrack", () => {
    it("counts steps and the domains registered so far while registering", () => {
        const order: Order = {
            id: "o1",
            status: "processing",
            created_at: "",
            completed_at: null,
            due_now_micros: 0,
            charged_micros: 0,
            refunded_micros: 0,
            steps: STEP_KEYS.map((key) => ({ key, status: key === "register" ? "running" : "pending", at: null })),
            domains: [domain("getacme.com", "active"), domain("tryacme.com", "registering"), domain("useacme.com", "registering")],
            mailboxes: [],
        };
        render(<SetupTrack order={order} loading={false} onBuyMore={() => {}} />);
        expect(screen.getByRole("progressbar", { name: "Setup progress" })).toHaveAttribute("aria-valuenow", "0");
        expect(screen.getByRole("heading", { name: "Registering 3 domains…" })).toBeInTheDocument();
        expect(screen.getByText(/of 3 registered/)).toBeInTheDocument();
        expect(screen.getByText(/Asking the registries/)).toBeInTheDocument();
    });
});
