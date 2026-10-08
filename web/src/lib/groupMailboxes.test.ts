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
    it("assigns overlapping tags to the first tag in the workspace order and ignores assignment order", () => {
        const rows = [mailbox("b", ["receiving"]), mailbox("a", ["receiving", "sending"]), mailbox("c", ["deleted"])];
        expect(groupMailboxes(rows, tags).map((row) => row.id)).toEqual(["a", "b", "c"]);
        expect(groupMailboxes(rows, [...tags].reverse()).map((row) => row.id)).toEqual(["a", "b", "c"]);
    });
});
