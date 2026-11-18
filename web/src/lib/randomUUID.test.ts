import { webcrypto } from "node:crypto";
import { describe, expect, it } from "vitest";

import { installRandomUUID } from "./randomUUID";

describe("installRandomUUID", () => {
    it("gives an insecure-context crypto a v4 randomUUID", () => {
        const c = { getRandomValues: webcrypto.getRandomValues.bind(webcrypto) } as unknown as Crypto;
        installRandomUUID(c);
        const a = c.randomUUID();
        expect(a).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
        expect(c.randomUUID()).not.toBe(a);
    });

    it("leaves a native randomUUID alone", () => {
        const native = () => "native" as `${string}-${string}-${string}-${string}-${string}`;
        const c = { randomUUID: native, getRandomValues: webcrypto.getRandomValues.bind(webcrypto) } as unknown as Crypto;
        installRandomUUID(c);
        expect(c.randomUUID).toBe(native);
    });
});
