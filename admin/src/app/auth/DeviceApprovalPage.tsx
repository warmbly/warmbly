import { useEffect, useRef, useState, type FormEvent } from "react";
import { ShieldCheck } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Callout, Panel, Property, PropertyList, StatusBadge } from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import { APIError, promptForReauth } from "@/lib/api/client";
import { describeAdminDevice, decideAdminDevice, type DeviceDescription } from "@/lib/api/client/admin/device";
import { API_URL } from "@/lib/env";
import { AdminPerm } from "@/lib/auth/permissions";
import { validDeviceDescription } from "@/lib/adminDevice";

type Phase = "entry" | "pending" | "approved" | "denied" | "expired" | "resolved";
type Review = Omit<DeviceDescription["request"], "consent_token"> & { permissions: number; requestedAccess: string };

export default function DeviceApprovalPage() {
    const [code, setCode] = useState("");
    const [review, setReview] = useState<Review | null>(null);
    const [phase, setPhase] = useState<Phase>("entry");
    const [busy, setBusy] = useState(false);
    const [needsReauth, setNeedsReauth] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const consent = useRef("");
    const submittedCode = useRef("");
    const inFlight = useRef(false);
    const mounted = useRef(true);

    useEffect(() => {
        mounted.current = true;
        return () => { mounted.current = false; consent.current = ""; };
    }, []);
    useEffect(() => {
        if (phase !== "pending" || !review) return;
        const expire = () => { consent.current = ""; setPhase("expired"); setNeedsReauth(false); };
        const remaining = Date.parse(review.expires_at) - Date.now();
        const timer = window.setTimeout(expire, Math.max(0, remaining));
        return () => window.clearTimeout(timer);
    }, [phase, review]);

    function failed(err: unknown) {
        setError(err);
        if (err instanceof APIError && err.code === "reauth_required") {
            setNeedsReauth(true);
        } else {
            consent.current = "";
            if (err instanceof APIError && err.code === "admin_device_expired") setPhase("expired");
            else if (err instanceof APIError && err.code === "admin_device_resolved") setPhase("resolved");
            else setPhase("entry");
        }
    }

    async function describe(e: FormEvent) {
        e.preventDefault();
        if (inFlight.current) return;
        inFlight.current = true;
        setBusy(true); setError(null); setReview(null); setNeedsReauth(false); consent.current = "";
        try {
            const normalized = code.trim().toUpperCase();
            const result = await describeAdminDevice(normalized);
            if (!mounted.current) return;
            if (!validDeviceDescription(result, API_URL)) throw new Error("The request identity or response could not be verified. Check this admin panel's configured backend before trying again.");
            const { consent_token, ...details } = result.request;
            consent.current = consent_token;
            submittedCode.current = normalized;
            setReview({ ...details, permissions: result.admin_permissions, requestedAccess: result.requested_access });
            setPhase(Date.parse(details.expires_at) <= Date.now() ? "expired" : "pending");
            if (Date.parse(details.expires_at) <= Date.now()) consent.current = "";
        } catch (err) { if (mounted.current) failed(err); }
        finally { inFlight.current = false; if (mounted.current) setBusy(false); }
    }

    async function decide(decision: "approved" | "denied") {
        if (inFlight.current || phase !== "pending" || !review || !consent.current || needsReauth) return;
        if (Date.parse(review.expires_at) <= Date.now()) { consent.current = ""; setPhase("expired"); return; }
        inFlight.current = true; setBusy(true); setError(null);
        try {
            const result = await decideAdminDevice(submittedCode.current, consent.current, decision);
            if (!mounted.current) return;
            if (result.status !== decision) throw new Error("The decision was not confirmed. Review a new request before trying again.");
            consent.current = ""; setPhase(result.status);
        } catch (err) { if (mounted.current) failed(err); }
        finally { inFlight.current = false; if (mounted.current) setBusy(false); }
    }

    async function reauthenticate() {
        if (inFlight.current) return;
        inFlight.current = true; setBusy(true); setError(null);
        try {
            await promptForReauth();
            if (mounted.current) setNeedsReauth(false);
        } catch (err) { if (mounted.current) setError(err); }
        finally { inFlight.current = false; if (mounted.current) setBusy(false); }
    }

    return <div className="max-w-3xl">
        <PageHeader title="Approve CLI sign-in" description="Authorize a separate platform administrator session only for a CLI request you started." />
        <Callout tone="warning" icon={ShieldCheck} title="Only approve your own request" className="mb-6">
            Never enter a code someone else sent you. Check the instance and requester against your terminal. Client names are requester-supplied, not verified device identities.
        </Callout>
        {error != null && <ErrorState error={error} title="Sign-in request could not be completed" className="mb-6" />}
        {phase === "entry" && <Panel title="Enter the code from your CLI">
            <form onSubmit={describe} className="space-y-4">
                <Label htmlFor="cli-code">User code</Label>
                <Input id="cli-code" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="off" autoCapitalize="characters" maxLength={9} placeholder="ABCD-EFGH" data-ph-mask="" disabled={busy} />
                <p className="text-sm text-muted-foreground">Reviewing a code does not approve it. No CLI credentials are displayed or stored here.</p>
                <Button type="submit" disabled={busy || !/^[A-Z2-9]{4}-?[A-Z2-9]{4}$/i.test(code.trim())}>{busy ? "Looking up…" : "Review request"}</Button>
            </form>
        </Panel>}
        {phase === "pending" && review && <Panel title="Review requested access">
            <StatusBadge tone="warning">Pending explicit approval</StatusBadge>
            <PropertyList className="mt-4">
                <Property label="Instance"><span className="break-all font-mono text-xs">{review.instance_url}</span></Property>
                <Property label="Requester client">{review.client_name}</Property>
                <Property label="Capability">{review.requestedAccess}</Property>
                <Property label="Permissions"><span>{Object.entries(AdminPerm).filter(([, bit]) => (review.permissions & bit) === bit).map(([name]) => name.replace(/([a-z])([A-Z])/g, "$1 $2").toLowerCase()).join(", ")} (mask {review.permissions})</span></Property>
                <Property label="Expires"><time dateTime={review.expires_at}>{new Date(review.expires_at).toLocaleString()}</time></Property>
            </PropertyList>
            <p className="my-4 text-sm text-muted-foreground">The CLI receives a separate, normally revocable session. Approval is not confirmation that the CLI has claimed it. Its original login proof can expire while it waits.</p>
            {needsReauth ? <div className="space-y-3">
                <p role="status" className="text-sm">Confirm your identity, then explicitly choose approve or deny again. Confirmation alone never approves this request.</p>
                <Button disabled={busy} onClick={() => void reauthenticate()}>Confirm identity</Button>
            </div> : <div className="flex gap-2">
                <Button disabled={busy} onClick={() => void decide("approved")}>{busy ? "Submitting…" : "Approve CLI sign-in"}</Button>
                <Button variant="outline" disabled={busy} onClick={() => void decide("denied")}>Deny request</Button>
            </div>}
        </Panel>}
        {["approved", "denied", "expired", "resolved"].includes(phase) && <Panel title={phase === "approved" ? "Request approved" : phase === "denied" ? "Request denied" : phase === "expired" ? "Request expired or unavailable" : "Request already resolved"}>
            <p role="status" className="text-sm text-muted-foreground">{phase === "approved" ? "Return to your CLI to finish sign-in. No access or refresh credentials are returned to this page. If claiming fails or the original proof expires, start a new request." : phase === "denied" ? "This request cannot sign in. Start a new request in your CLI if needed." : "This code can no longer be used. Start a new request in your CLI, then review its new code here."}</p>
            <Button variant="outline" className="mt-4" disabled={busy} onClick={() => { consent.current = ""; setReview(null); setCode(""); setError(null); setPhase("entry"); }}>Enter another code</Button>
        </Panel>}
    </div>;
}
