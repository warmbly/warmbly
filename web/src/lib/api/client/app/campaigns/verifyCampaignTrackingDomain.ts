import Request from "../../Request";

// Mirrors backend models.TrackingDomainStatus (bare object response).
export interface TrackingDomainStatus {
    tracking_domain: string;
    tracking_domain_verified: boolean;
    tracking_domain_verified_at?: string | null;
}

// POST /campaigns/:id/tracking-domain/verify — resolves the CNAME and flips
// verified on success. Side-effectful but naturally idempotent (re-resolving
// the same domain is safe) and covered by the global Idempotency-Key middleware.
export default async function verifyCampaignTrackingDomain(campaignId: string): Promise<TrackingDomainStatus> {
    return await Request<TrackingDomainStatus>({
        method: "POST",
        url: `/campaigns/${campaignId}/tracking-domain/verify`,
        authorization: true,
    });
}
