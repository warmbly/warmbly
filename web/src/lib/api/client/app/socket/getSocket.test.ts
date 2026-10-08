import { describe, expect, it } from "vitest";
import { socketURL } from "./getSocket";

describe("realtime upgrade routing", () => {
    const ticket = { url: "wss://old.realtime.test/socket/websocket?token=ticket", expires_in: 600 };
    it("keeps direct URLs from old backends", () => {
        expect(socketURL(ticket, "https://api.test/v1")).toBe(ticket.url);
    });
    it.each(["https://api.test/v1", "https://api.test/v1/"])("uses the API CSP origin with %s", (base) => {
        expect(socketURL({ ...ticket, proxy_path: "/realtime/socket/websocket?token=ticket" }, base))
            .toBe("wss://api.test/v1/realtime/socket/websocket?token=ticket");
    });
    it("preserves plain HTTP ports and same-origin API deployments", () => {
        expect(socketURL({ ...ticket, proxy_path: "/realtime/socket/websocket?token=ticket" }, "http://localhost:8080/v1"))
            .toBe("ws://localhost:8080/v1/realtime/socket/websocket?token=ticket");
        expect(socketURL({ ...ticket, proxy_path: "/realtime/socket/websocket?token=ticket" }, "/v1"))
            .toContain("/v1/realtime/socket/websocket?token=ticket");
    });
    it("refuses another origin supplied as a proxy path", () => {
        expect(() => socketURL({ ...ticket, proxy_path: "https://untrusted.test/socket" }, "https://api.test/v1")).toThrow();
    });
});
