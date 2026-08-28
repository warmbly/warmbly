// App-wide banner for a workspace whose sending is restricted or suspended.
//
// Without it the only symptom is volume quietly dropping, or campaigns that
// never send, with nothing anywhere saying why. Not dismissible: it describes
// an active limit on the account, not a notice.

import { AlertTriangleIcon, OctagonXIcon } from "lucide-react";
import useOrganizationRisk from "@/lib/api/hooks/app/organizations/useOrganizationRisk";

export default function SendingRestrictedBar() {
    const { data } = useOrganizationRisk();
    if (!data || (!data.restricted && !data.suspended)) return null;

    const suspended = data.suspended;
    const Icon = suspended ? OctagonXIcon : AlertTriangleIcon;

    return (
        <div
            role="status"
            className={`flex items-start gap-2 px-4 py-2.5 border-b ${
                suspended
                    ? "bg-rose-50 border-rose-200 text-rose-900"
                    : "bg-amber-50 border-amber-200 text-amber-900"
            }`}
        >
            <Icon className={`w-4 h-4 mt-px shrink-0 ${suspended ? "text-rose-600" : "text-amber-600"}`} />
            <div className="min-w-0 text-[12.5px] leading-relaxed">
                <span className="font-medium">
                    {suspended
                        ? "Sending is paused for this workspace while it is reviewed."
                        : "Sending from this workspace is limited while it is reviewed."}
                </span>{" "}
                {!suspended && (
                    <>
                        Daily volume per mailbox is reduced and warmup runs in the shared free pool.{" "}
                    </>
                )}
                {data.reason ? <>Reason: {data.reason}. </> : null}
                Contact support if you think this is wrong.
            </div>
        </div>
    );
}
