import { describe, expect, it } from "vitest";
import type Inbox from "./api/models/app/emails/Inbox";
import type Tag from "./api/models/app/Tag";
import groupMailboxes from "./groupMailboxes";

describe("default mailbox tag groups", () => {
    const tags = [{ id: "sending", title: "Sending", position: 0 }, { id: "receiving", title: "Receiving", position: 1 }] as Tag[];
    const mailbox = (id: string, assigned: string[]) => ({ id, email: `${id}@test.example`, tags: assigned }) as Inbox;
    it("keeps shared tags together with untagged mailboxes last without mutating cached data", () => {
        const rows = [mailbox("z", []), mailbox("c", ["receiving"]), mailbox("b", ["sending"]), mailbox("a", ["sending"])];
        expect(groupMailboxes(rows, tags).map((row) => row.id)).toEqual(["a", "b", "c", "z"]);
        expect(rows.map((row) => row.id)).toEqual(["z", "c", "b", "a"]);
    });
    it("orders tag sets by workspace order rather than assignment order", () => {
        const rows = [mailbox("b", ["receiving"]), mailbox("a", ["receiving", "sending"]), mailbox("c", ["deleted"])];
        expect(groupMailboxes(rows, tags).map((row) => row.id)).toEqual(["a", "b", "c"]);
        expect(groupMailboxes(rows, [...tags].reverse()).map((row) => row.id)).toEqual(["a", "b", "c"]);
    });
    it("keeps all four identical multi-tag sets together instead of using only their first tag", () => {
        const allTags = [...tags, { id: "other", title: "Other", position: 2 } as Tag];
        const rows = [
            mailbox("h", ["receiving", "sending"]), mailbox("a", ["sending"]),
            mailbox("e", ["sending", "receiving"]), mailbox("d", ["sending", "other"]),
            mailbox("b", ["receiving", "sending"]), mailbox("z", []),
            mailbox("c", ["sending"]), mailbox("f", ["sending", "receiving"]),
        ];
        const before = rows.map((row) => [...row.tags]);
        expect(groupMailboxes(rows, allTags).map((row) => row.id)).toEqual(["a", "c", "b", "e", "f", "h", "d", "z"]);
        expect(rows.map((row) => row.tags)).toEqual(before);
    });
    it("treats duplicate assignments as a set and preserves unknown tag identities", () => {
        const rows = [
            mailbox("d", ["missing-y"]), mailbox("b", ["missing-x"]),
            mailbox("c", ["missing-y", "missing-y"]), mailbox("a", ["missing-x"]),
            mailbox("f", ["sending", "sending"]), mailbox("e", ["sending"]), mailbox("z", []),
        ];
        expect(groupMailboxes(rows, tags).map((row) => row.id)).toEqual(["e", "f", "a", "b", "c", "d", "z"]);
    });
    it("uses stable email and id ordering inside each tag set, including an empty workspace", () => {
        const rows = [mailbox("b", []), mailbox("a", []), mailbox("c", [])];
        rows[0].email = rows[1].email;
        expect(groupMailboxes(rows, []).map((row) => row.id)).toEqual(["a", "b", "c"]);
    });
});
