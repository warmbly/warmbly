import { z } from "zod";

export const browseString = z.string();
export const browseBoolean = z.boolean();
export const analyticsRange = z.enum(["7d", "30d", "90d"]);
export const mailboxTab = z.enum(["overview", "deliverability", "analytics", "warmup", "sending", "settings"]);
export type MailboxTab = z.infer<typeof mailboxTab>;
export const domainTab = z.enum(["overview", "tracking", "redirect"]);
export const domainFilter = z.enum(["all", "attention", "vendor"]);
export const mailboxSort = z.strictObject({
    by: z.enum(["mailbox", "status", "sent", "warmup", "inbox", "health"]),
    reverse: z.boolean(),
}).nullable();
export const analyticsHiddenMetrics = z.array(z.enum(["sent", "opens", "clicks", "replies"]))
    .max(3).refine((values) => new Set(values).size === values.length);
export const deliverabilityHiddenMetrics = z.array(z.enum(["bounces", "complaints", "opens", "replies", "sent"]))
    .max(4).refine((values) => new Set(values).size === values.length);
export const deliverabilityView = z.strictObject({
    range: analyticsRange,
    hidden: z.array(z.enum(["chart", "totals", "placement", "providers", "mailboxes", "campaigns"]))
        .max(6).refine((values) => new Set(values).size === values.length),
});
export const placementRange = z.enum(["7d", "14d", "30d", "90d"]);
export const placementGroup = z.enum(["google", "microsoft", "yahoo", "other"]);
export const placementGroupFilter = z.enum(["all", "google", "microsoft", "yahoo", "other"]);
export const batchTab = z.enum(["mailboxes", "domains", "providers", "recipients"]);
export const batchSenderSort = z.enum(["worst", "best", "email", "status"]);
export const batchSenderStatus = z.enum(["", "queued", "deferred", "running", "completed", "skipped", "failed", "cancelled"]);
export const importFilter = z.strictObject({
    status: z.enum(["", "queued", "running", "connected", "updated", "skipped", "failed", "needs_signin", "cancelled"]),
    cause: z.string(),
}).refine((value) => !value.cause || value.status === "failed");
export const importPickFilter = z.enum(["all", "new", "moving", "connected"]);
export const domainListLimit = z.number().finite().int().positive();
