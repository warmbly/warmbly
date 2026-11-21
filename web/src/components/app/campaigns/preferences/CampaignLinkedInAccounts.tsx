// The LinkedIn accounts that run a campaign's LinkedIn steps.
import { Link } from "@tanstack/react-router";
import { Checkbox } from "@/components/ui/checkbox";
import { Avatar, StatusPill } from "@/components/app/linkedin/parts";
import LinkedInMark from "@/components/app/linkedin/LinkedInMark";
import { useLinkedInAccounts } from "@/lib/api/hooks/app/linkedin/useLinkedIn";

export function LinkedInAccountsSection({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
    const { data: accounts, isPending, isError } = useLinkedInAccounts();
    const picked = new Set(value);
    // An account removed since it was picked still shows, so it can be taken off.
    const listed = (accounts?.data ?? []).filter((a) => a.status !== "removed" || picked.has(a.id));
    const toggle = (id: string) => onChange(picked.has(id) ? value.filter((x) => x !== id) : [...value, id]);

    if (isPending) return <div className="h-16 rounded-md bg-slate-50 animate-pulse" />;
    if (isError) return <p className="text-[12px] text-rose-600">The LinkedIn accounts could not be loaded. Refresh to try again.</p>;
    if (listed.length === 0) {
        return (
            <div className="flex items-center gap-3 rounded-md border border-dashed border-slate-200 px-4 py-3">
                <LinkedInMark className="size-4 shrink-0" />
                <p className="min-w-0 flex-1 text-[12px] text-slate-600">No LinkedIn account is connected yet. LinkedIn steps need one to run.</p>
                <Link to="/app/linkedin" className="h-7 shrink-0 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] font-medium text-slate-700 inline-flex items-center hover:bg-slate-50">
                    Connect LinkedIn
                </Link>
            </div>
        );
    }
    return (
        <div className="space-y-2">
            <div className="divide-y divide-slate-100 rounded-md border border-slate-200">
                {listed.map((a) => (
                    <label key={a.id} className="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-slate-50/60">
                        <Checkbox tone="slate" checked={picked.has(a.id)} onChange={() => toggle(a.id)} aria-label={`Use ${a.display_name}`} />
                        <Avatar src={a.avatar_url} name={a.display_name} size={28} />
                        <span className="min-w-0 flex-1">
                            <span className="block truncate text-[12.5px] font-medium text-slate-900">{a.display_name}</span>
                            {a.headline && <span className="block truncate text-[11.5px] text-slate-500">{a.headline}</span>}
                        </span>
                        <StatusPill status={a.status} className="shrink-0" />
                    </label>
                ))}
            </div>
            <p className="text-[11.5px] leading-relaxed text-slate-500">
                {value.length === 0
                    ? "Pick at least one account, or the campaign cannot launch with LinkedIn steps."
                    : "Each contact stays with the first account that reached them. New contacts go to the account with the most room left today."}
            </p>
        </div>
    );
}
