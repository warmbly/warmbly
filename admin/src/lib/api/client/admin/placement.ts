// /admin/placement/* — seed inbox-placement testing.
//
// A placement test sends a tokenized copy of a template through a real sender
// to a panel of Warmbly-controlled SEED mailboxes; a backend poller then looks
// the token up in each seed's synced inbox (the unibox) and classifies where it
// landed (Inbox / Spam / Promotions / other), per provider. This module shapes
// the requests and returns the backend's `{ data }` / `{ pagination }`
// envelopes. Types are co-located here since this surface is self-contained.

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";

// --------------------------------------------------------------------
// Types
// --------------------------------------------------------------------

export type PlacementFolder =
    | "inbox"
    | "promotions"
    | "spam"
    | "other"
    | "pending";

export type PlacementStatus = "pending" | "completed";

export interface PlacementTestRow {
    id: string;
    organization_id: string | null;
    sender_account_id: string;
    subject: string;
    status: PlacementStatus;
    created_at: string;
    finished_at: string | null;
}

export interface PlacementResultRow {
    seed_account_id: string;
    provider: string;
    folder: PlacementFolder;
    detected_at: string | null;
    raw_flags: string;
}

export interface PlacementProviderRollup {
    provider: string;
    inbox: number;
    promotions: number;
    spam: number;
    other: number;
    pending: number;
    total: number;
}

export interface PlacementTestDetail {
    test: PlacementTestRow;
    rollup: PlacementProviderRollup[];
    results: PlacementResultRow[];
}

export interface SeedAccount {
    id: string;
    email: string;
    name: string;
    provider: string;
    status: string;
    worker_id: string | null;
    is_seed: boolean;
}

export interface Pagination {
    total: number;
    has_more: boolean;
    next_cursor: string | null;
}

export interface PlacementTestsResult {
    data: PlacementTestRow[];
    pagination: Pagination;
}

export interface CreatePlacementTestRequest {
    sender_account_id: string;
    subject: string;
    body_plain: string;
    body_html: string;
}

export interface ListTestsParams {
    cursor?: string;
    limit?: number;
}

// --------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------

export function listPlacementTests(
    params: ListTestsParams = {},
): Promise<PlacementTestsResult> {
    return Request({
        method: "GET",
        url: `/admin/placement/tests${buildSearchQuery(
            params as Record<string, unknown>,
        )}`,
        authorization: true,
    });
}

export function getPlacementTest(
    id: string,
): Promise<{ data: PlacementTestDetail }> {
    return Request({
        method: "GET",
        url: `/admin/placement/tests/${id}`,
        authorization: true,
    });
}

export function createPlacementTest(
    body: CreatePlacementTestRequest,
): Promise<{ data: PlacementTestRow }> {
    return Request({
        method: "POST",
        url: "/admin/placement/tests",
        data: body,
        authorization: true,
    });
}

// --------------------------------------------------------------------
// Seeds
// --------------------------------------------------------------------

export function listSeedMailboxes(): Promise<{ data: SeedAccount[] }> {
    return Request({
        method: "GET",
        url: "/admin/placement/seeds",
        authorization: true,
    });
}

export function listSeedCandidates(
    search?: string,
): Promise<{ data: SeedAccount[] }> {
    return Request({
        method: "GET",
        url: `/admin/placement/seeds/candidates${buildSearchQuery({ search })}`,
        authorization: true,
    });
}

export function setSeedMailbox(
    id: string,
    isSeed: boolean,
): Promise<{ ok: true }> {
    return Request({
        method: "POST",
        url: `/admin/placement/seeds/${id}`,
        data: { is_seed: isSeed },
        authorization: true,
    });
}
