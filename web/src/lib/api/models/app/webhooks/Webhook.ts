// Webhook management types (mirror of the Go backend's webhook models).
//
// Endpoints receive HMAC-signed HTTP callbacks when workspace events fire.
// The signing secret is returned exactly once on create / rotate; deliveries
// are the per-attempt log; drops record high-volume events that were throttled.

export type WebhookDeliveryStatus =
    | "pending"
    | "in_flight"
    | "delivered"
    | "failed"
    | "abandoned";

export interface WebhookEndpoint {
    id: string;
    organization_id: string;
    url: string;
    description: string;
    event_types: string[];
    enabled: boolean;
    last_success_at?: Date;
    last_failure_at?: Date;
    last_failure_reason?: string;
    consecutive_failures: number;
    oauth_application_id?: string;
    created_by?: string;
    verified_at?: Date;
    ownership_confirmed: boolean;
    auto_disabled_at?: Date;
    disabled_reason?: string;
    created_at: Date;
    updated_at: Date;
}

// Returned once on create; secret is shown a single time and never again.
export interface WebhookEndpointWithSecret extends WebhookEndpoint {
    secret: string;
}

// One subscribable event in the catalog. `firehose` events are high-volume and
// only delivered when an endpoint subscribes to them explicitly.
export interface WebhookEventDescriptor {
    type: string;
    category: string;
    description: string;
    firehose: boolean;
    // Never delivered; kept valid so saved subscriptions still save. Absent from older backends.
    deprecated?: boolean;
}

export interface WebhookDelivery {
    id: string;
    endpoint_id: string;
    organization_id: string;
    event_type: string;
    event_id: string;
    payload: unknown;
    status: WebhookDeliveryStatus;
    attempt_count: number;
    max_attempts: number;
    next_attempt_at: Date;
    last_attempt_at?: Date;
    response_status?: number;
    response_body_excerpt?: string;
    error_reason?: string;
    created_at: Date;
    updated_at: Date;
}

// A high-volume event family that was throttled (dropped windows) for a day.
export interface WebhookEventDrop {
    event_type: string;
    day: Date;
    dropped_windows: number;
    last_dropped_at: Date;
}

// GET /webhooks
export interface WebhooksResult {
    endpoints: WebhookEndpoint[];
    event_types: WebhookEventDescriptor[];
}

// GET /webhooks/event-types
export interface WebhookEventTypesResult {
    event_types: WebhookEventDescriptor[];
}

// GET /webhooks/deliveries (and /webhooks/:id/deliveries)
export interface WebhookDeliveriesResult {
    data: WebhookDelivery[];
    pagination: {
        next_cursor: string | null;
        has_more: boolean;
    };
}

// GET /webhooks/throttle-drops
export interface WebhookDropsResult {
    drops: WebhookEventDrop[];
}

// POST / PATCH /webhooks body.
export interface WebhookEndpointInput {
    url: string;
    description?: string;
    event_types: string[];
    enabled?: boolean;
}

// Filters for the delivery log query.
export interface WebhookDeliveriesQuery {
    endpointId?: string;
    status?: WebhookDeliveryStatus | "";
    eventType?: string;
    cursor?: string;
    limit?: number;
}
