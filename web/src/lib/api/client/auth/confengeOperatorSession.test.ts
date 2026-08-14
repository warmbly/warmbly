import { beforeEach, describe, expect, it, vi } from "vitest";
import { clearTokens } from "@/lib/auth";

const mocks = vi.hoisted(() => ({ post: vi.fn() }));

vi.mock("../Client", () => ({ default: { post: mocks.post } }));

import { ensureConfengeOperatorSession } from "./confengeOperatorSession";

describe("sessão silenciosa do operador CONFENGE", () => {
  beforeEach(() => {
    clearTokens();
    mocks.post.mockReset();
    vi.mocked(localStorage.setItem).mockClear();
  });

  it("deduplica solicitações simultâneas e persiste os tokens", async () => {
    mocks.post.mockResolvedValue({
      data: {
        access_token: "access",
        access_token_expires_at: "2099-01-01T00:00:00Z",
        refresh_token: "refresh",
        refresh_token_expires_at: "2099-02-01T00:00:00Z",
      },
    });

    const [first, second] = await Promise.all([
      ensureConfengeOperatorSession(),
      ensureConfengeOperatorSession(),
    ]);

    expect(mocks.post).toHaveBeenCalledTimes(1);
    expect(first.access_token).toBe("access");
    expect(second.access_token).toBe("access");
    expect(localStorage.setItem).toHaveBeenCalledWith("auth_token", expect.stringContaining("access"));
  });

  it("substitui uma sessão válida quando o layout força a identidade técnica", async () => {
    localStorage.setItem("auth_token", JSON.stringify({
      access_token: "wrong-org",
      access_token_expires_at: "2099-01-01T00:00:00Z",
    }));
    mocks.post.mockResolvedValue({
      data: {
        access_token: "operator",
        access_token_expires_at: "2099-01-01T00:00:00Z",
        refresh_token: "operator-refresh",
        refresh_token_expires_at: "2099-02-01T00:00:00Z",
      },
    });

    const session = await ensureConfengeOperatorSession(true);

    expect(mocks.post).toHaveBeenCalledTimes(1);
    expect(session.access_token).toBe("operator");
  });
});
