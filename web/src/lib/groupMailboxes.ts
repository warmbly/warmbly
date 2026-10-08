import type Inbox from "./api/models/app/emails/Inbox";
import type Tag from "./api/models/app/Tag";

export default function groupMailboxes(mailboxes: readonly Inbox[], tags: readonly Tag[]): Inbox[] {
    const ordered = [...tags].sort((a, b) => a.position - b.position || a.title.localeCompare(b.title) || a.id.localeCompare(b.id));
    const rank = new Map(ordered.map((tag, index) => [tag.id, index]));
    const compareTag = (a: string, b: string) =>
        (rank.get(a) ?? ordered.length) - (rank.get(b) ?? ordered.length) || a.localeCompare(b);
    const groups = mailboxes.map((mailbox) => ({ mailbox, tags: [...new Set(mailbox.tags)].sort(compareTag) }));
    return groups.sort((a, b) => {
        if (!a.tags.length && b.tags.length) return 1;
        if (a.tags.length && !b.tags.length) return -1;
        for (let i = 0; i < Math.min(a.tags.length, b.tags.length); i++) {
            const order = compareTag(a.tags[i], b.tags[i]);
            if (order) return order;
        }
        return a.tags.length - b.tags.length || a.mailbox.email.localeCompare(b.mailbox.email) || a.mailbox.id.localeCompare(b.mailbox.id);
    }).map(({ mailbox }) => mailbox);
}
