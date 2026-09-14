import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";

/**
 * conversationSubjectFor is the subject a step at `index` would send when it
 * replies in the contact's thread: the subject of the step that opened that
 * conversation. Walking back, every step that also replies passes the question
 * on; the first that does not is the one that opened the thread.
 *
 * `null` means there is no earlier email step, so this one opens the
 * conversation whatever its switch says. An empty string means the opener
 * itself has no subject yet.
 *
 * It mirrors the walk the send path does over the contact's actual sends, so
 * the composer shows what the recipient will read. It follows the canvas order
 * rather than branch targets: a step several branches reach can be opened by a
 * different email on each path, and the editor has to show one answer. The
 * send path resolves it per contact from what they were actually sent, so this
 * is a preview, and for a linear sequence the two always agree.
 */
export function conversationSubjectFor(steps: Sequence[], index: number): string | null {
    let subject: string | null = null;
    for (let i = index - 1; i >= 0; i--) {
        const step = steps[i];
        if (step.kind !== "email") continue;
        subject = step.subject;
        if (!step.thread_reply) break;
    }
    return subject;
}
