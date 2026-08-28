// Result of POST /campaigns/{id}/preflight — the platform's own pre-send
// checks, run before a campaign starts sending.

export interface PreflightCheckResult {
    key: string;
    passed: boolean;
    severity: string; // "error" | "warning" | "info"
    message: string;
    remediation?: string;
}

export interface PreflightReport {
    id: string;
    organization_id: string;
    campaign_id: string;
    passed: boolean;
    score: number;
    checks: PreflightCheckResult[];
    recommendations: string[];
    created_at: string;
}

// Only failures are worth a user's attention; a passing check is noise in a
// launch dialog.
export function preflightFailures(r: PreflightReport | undefined): PreflightCheckResult[] {
    return (r?.checks ?? []).filter((c) => !c.passed);
}

export function hasBlockingFailure(r: PreflightReport | undefined): boolean {
    return preflightFailures(r).some((c) => c.severity === "error");
}
