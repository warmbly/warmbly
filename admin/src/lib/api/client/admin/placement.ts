// /admin/placement/*: the instance seed panel and every workspace's inbox
// placement tests. Shapes mirror internal/app/placement/view.go and
// internal/models/placement.go.

import { Request } from "@/lib/api/client";

export type PlacementPanel = "instance" | "workspace" | "cloud";
export type PlacementOrigin = "manual" | "monitor" | "admin" | "remote";
export type PlacementStatus = "running" | "completed" | "cancelled" | "failed";
export type PlacementTracking = "campaign" | "on" | "off" | "compare";
export type PlacementFolder =
    | "pending"
    | "inbox"
    | "promotions"
    | "other"
    | "spam"
    | "missing"
    | "failed"
    | "cancelled";

export interface PlacementCounts {
    total: number;
    pending: number;
    inbox: number;
    promotions: number;
    other: number;
    spam: number;
    missing: number;
    failed: number;
    cancelled: number;
    delivered: number;
    // Fractions 0..1 of delivered; null until something is delivered.
    inbox_rate: number | null;
    tabs_rate: number | null;
    spam_rate: number | null;
    missing_rate: number | null;
}

export interface PlacementFamilyCounts {
    family: string;
    label: string;
    counts: PlacementCounts;
}

export interface PlacementTestView {
    id: string;
    sender_account_id: string | null;
    sender_email: string;
    created_by: string | null;
    campaign_id: string | null;
    sequence_id: string | null;
    contact_id: string | null;
    monitor_id: string | null;
    subject: string;
    body_html?: string;
    body_plain?: string;
    open_tracking: boolean;
    link_tracking: boolean;
    compare_group_id: string | null;
    origin: PlacementOrigin;
    panel: PlacementPanel;
    status: PlacementStatus;
    error?: string;
    created_at: string;
    finished_at: string | null;
    summary: PlacementCounts;
    families: PlacementFamilyCounts[] | null;
}

export interface PlacementResultView {
    seed: string;
    family: string;
    family_label: string;
    folder: PlacementFolder;
    scheduled_at: string | null;
    sent_at: string | null;
    detected_at: string | null;
    error?: string;
}

export interface PlacementContentIssue {
    severity: "warn" | "high";
    code: string;
    message: string;
    field?: string;
    suggestion?: string;
}

export interface PlacementTestDetail extends PlacementTestView {
    results: PlacementResultView[] | null;
    content: { score: number; issues: PlacementContentIssue[] | null };
    compare?: PlacementTestView;
}

export type AdminSeedScope = "" | "instance" | "workspace";

export interface AdminPlacementSeed {
    id: string;
    organization_id: string | null;
    email: string;
    name: string;
    provider: string;
    family: string;
    family_label: string;
    status: string;
    worker_id: string | null;
    seed_scope: AdminSeedScope;
}

export interface PlacementTestsPage {
    data: PlacementTestView[] | null;
    pagination: { total: number; has_more: boolean; next_cursor: string | null };
}

export interface AdminCreatePlacementTestBody {
    sender_account_id: string;
    subject: string;
    body_html?: string;
    body_plain?: string;
    tracking?: PlacementTracking;
}

export function listPlacementTests(cursor?: string, limit = 25): Promise<PlacementTestsPage> {
    const usp = new URLSearchParams();
    if (cursor) usp.set("cursor", cursor);
    usp.set("limit", String(limit));
    return Request({
        method: "GET",
        url: `/admin/placement/tests?${usp.toString()}`,
        authorization: true,
    });
}

export function getPlacementTest(id: string): Promise<PlacementTestDetail> {
    return Request<{ data: PlacementTestDetail }>({
        method: "GET",
        url: `/admin/placement/tests/${id}`,
        authorization: true,
    }).then((r) => r.data);
}

export function createPlacementTest(body: AdminCreatePlacementTestBody): Promise<PlacementTestView[]> {
    return Request<{ data: PlacementTestView[] | null }>({
        method: "POST",
        url: "/admin/placement/tests",
        data: body,
        authorization: true,
    }).then((r) => r.data ?? []);
}

export function listPlacementSeeds(): Promise<AdminPlacementSeed[]> {
    return Request<{ data: AdminPlacementSeed[] | null }>({
        method: "GET",
        url: "/admin/placement/seeds",
        authorization: true,
    }).then((r) => r.data ?? []);
}

export function searchPlacementSeedCandidates(search: string, limit = 50): Promise<AdminPlacementSeed[]> {
    const usp = new URLSearchParams();
    if (search) usp.set("search", search);
    usp.set("limit", String(limit));
    return Request<{ data: AdminPlacementSeed[] | null }>({
        method: "GET",
        url: `/admin/placement/seeds/candidates?${usp.toString()}`,
        authorization: true,
    }).then((r) => r.data ?? []);
}

export function setPlacementSeed(id: string, seed: boolean): Promise<AdminPlacementSeed> {
    return Request<{ data: AdminPlacementSeed }>({
        method: "POST",
        url: `/admin/placement/seeds/${id}`,
        data: { seed },
        authorization: true,
    }).then((r) => r.data);
}
