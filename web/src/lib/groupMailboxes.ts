import type Inbox from "./api/models/app/emails/Inbox";
import type Tag from "./api/models/app/Tag";

export default function groupMailboxes(mailboxes: readonly Inbox[], tags: readonly Tag[]): Inbox[] {
    const ordered = [...tags].sort((a, b) => a.position - b.position || a.title.localeCompare(b.title) || a.id.localeCompare(b.id));
    const rank = new Map(ordered.map((tag, index) => [tag.id, index]));
    const group = (mailbox: Inbox) => Math.min(...mailbox.tags.map((id) => rank.get(id) ?? ordered.length), ordered.length);
    return [...mailboxes].sort((a, b) => group(a) - group(b) || a.email.localeCompare(b.email) || a.id.localeCompare(b.id));
}
