import toast from "react-hot-toast";
import { Monitor, Smartphone, Globe, LogOut } from "lucide-react";
import { Section } from "../_components/SectionShell";
import { Loading } from "@/components/loader";
import { useConfirm } from "@/hooks/context/confirm";
import useSessions from "@/lib/api/hooks/auth/useSessions";
import useRevokeSession from "@/lib/api/hooks/auth/useRevokeSession";
import useRevokeOtherSessions from "@/lib/api/hooks/auth/useRevokeOtherSessions";
import type ActiveSession from "@/lib/api/models/auth/ActiveSession";

const PROVIDER_LABELS: Record<string, string> = {
    email: "Email",
    google: "Google",
    apple: "Apple",
    webauthn: "Passkey",
};

function providerLabel(p: string): string | null {
    if (!p) return null;
    return PROVIDER_LABELS[p] ?? p;
}

function deviceLabel(s: ActiveSession): string {
    const browser = s.browser?.trim();
    const os = s.os?.trim();
    if (browser && os) return `${browser} on ${os}`;
    return browser || os || "Unknown device";
}

function locationLabel(s: ActiveSession): string {
    const parts = [s.location_city, s.location_region, s.location_country]
        .map((p) => p?.trim())
        .filter((p): p is string => !!p && p.toLowerCase() !== "unknown");
    return parts.length ? Array.from(new Set(parts)).join(", ") : "Unknown location";
}

function DeviceIcon({ os }: { os: string }) {
    const cls = "w-4 h-4 text-slate-500";
    if (!os) return <Globe className={cls} />;
    if (/ios|iphone|ipad|android/i.test(os)) return <Smartphone className={cls} />;
    return <Monitor className={cls} />;
}

function relTime(d: Date | string): string {
    const date = new Date(d);
    const sec = Math.floor((Date.now() - date.getTime()) / 1000);
    if (sec < 45) return "just now";
    const min = Math.floor(sec / 60);
    if (min < 60) return `${Math.max(min, 1)}m ago`;
    const hr = Math.floor(min / 60);
    if (hr < 24) return `${hr}h ago`;
    const day = Math.floor(hr / 24);
    if (day < 30) return `${day}d ago`;
    return date.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

export default function SessionManager() {
    const { data: sessions, isLoading } = useSessions();
    const revoke = useRevokeSession();
    const revokeOthers = useRevokeOtherSessions();
    const confirm = useConfirm();

    const others = (sessions ?? []).filter((s) => !s.current);

    const handleRevoke = (s: ActiveSession) => {
        confirm?.show(
            `Sign out ${deviceLabel(s)}? That device will need to sign in again.`,
            async () => {
                try {
                    await revoke.mutateAsync(s.id);
                    toast.success("Session signed out");
                } catch {
                    toast.error("Couldn't sign out that session.");
                }
            },
        );
    };

    const handleRevokeOthers = () => {
        confirm?.show(
            "Sign out of all other sessions? Every device except this one will need to sign in again.",
            async () => {
                try {
                    await revokeOthers.mutateAsync();
                    toast.success("Signed out everywhere else");
                } catch {
                    toast.error("Couldn't sign out the other sessions.");
                }
            },
        );
    };

    return (
        <Section
            eyebrow="Sessions"
            description="Devices currently signed in to your account. Sign out any you don't recognize."
        >
            <div className="space-y-3">
                {isLoading ? (
                    <div className="flex items-center gap-2 text-[12px] text-slate-400 py-2">
                        <Loading className="!w-4 h-4 text-slate-400" /> Loading sessions...
                    </div>
                ) : sessions && sessions.length > 0 ? (
                    <div className="rounded-md border border-slate-200 divide-y divide-slate-200 bg-white">
                        {sessions.map((s) => {
                            const provider = providerLabel(s.auth_provider);
                            return (
                                <div key={s.id} className="flex items-center gap-3 px-3 py-2.5">
                                    <div className="w-8 h-8 rounded-lg bg-slate-100 flex items-center justify-center shrink-0">
                                        <DeviceIcon os={s.os} />
                                    </div>
                                    <div className="min-w-0 flex-1">
                                        <div className="flex items-center gap-2">
                                            <span className="text-[12.5px] font-medium text-slate-900 truncate">
                                                {deviceLabel(s)}
                                            </span>
                                            {s.current && (
                                                <span className="text-[10px] uppercase tracking-[0.08em] font-medium rounded-sm px-1 bg-sky-50 text-sky-700">
                                                    This device
                                                </span>
                                            )}
                                        </div>
                                        <div className="text-[11px] text-slate-500 truncate mt-0.5">
                                            {locationLabel(s)}
                                            {provider ? ` · ${provider}` : ""}
                                            {" · "}
                                            {s.current ? "Active now" : `Active ${relTime(s.last_active_at)}`}
                                        </div>
                                    </div>
                                    {!s.current && (
                                        <button
                                            type="button"
                                            onClick={() => handleRevoke(s)}
                                            disabled={revoke.isPending}
                                            className="h-7 px-2.5 inline-flex items-center justify-center rounded-md text-[12px] text-slate-500 hover:text-red-600 hover:bg-red-50 transition-colors disabled:opacity-50 shrink-0"
                                        >
                                            Sign out
                                        </button>
                                    )}
                                </div>
                            );
                        })}
                    </div>
                ) : (
                    <p className="text-[12px] text-slate-500 leading-relaxed">
                        No active sessions found.
                    </p>
                )}

                {others.length > 0 && (
                    <button
                        type="button"
                        onClick={handleRevokeOthers}
                        disabled={revokeOthers.isPending}
                        className="h-8 px-3 rounded-md border border-slate-200 hover:border-red-300 hover:bg-red-50/50 text-[12.5px] font-medium text-slate-700 hover:text-red-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:pointer-events-none"
                    >
                        {revokeOthers.isPending ? (
                            <Loading className="!w-3.5 h-3.5 text-slate-500" />
                        ) : (
                            <LogOut className="w-3.5 h-3.5" />
                        )}
                        Sign out other sessions
                    </button>
                )}
            </div>
        </Section>
    );
}
