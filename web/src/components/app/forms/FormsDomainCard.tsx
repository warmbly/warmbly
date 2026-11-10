// FormsDomainCard — the workspace's custom forms domain. Shaped like the
// mailbox tracking-domain panel: type a subdomain, add the CNAME, verify. It
// is workspace-wide, so it appears on every form's Share tab and on Branding.
// The caller supplies the heading and the explanation; this renders controls only.
// CustomDomainPanel is the same controls for any custom host, such as an agency portal domain.

import React from "react";
import { CheckIcon, CopyIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { Label, TextInput } from "@/components/ui/field";
import { useWriteGuard } from "@/hooks/usePermission";
import { useFormsDomain, useSetFormsDomain, useVerifyFormsDomain } from "@/lib/api/hooks/app/forms";
import type { AppError } from "@/lib/api/client/normalizeError";
import type FormsDomainStatus from "@/lib/api/models/app/forms/FormsDomain";
import buildError from "@/lib/helper/buildError";

// Every status the backend can report, in the customer's words.
const TONE: Record<string, { pill: string; label: string }> = {
    verified: { pill: "bg-emerald-50 text-emerald-700", label: "Verified" },
    pending: { pill: "bg-amber-50 text-amber-700", label: "Not verified" },
    not_found: { pill: "bg-amber-50 text-amber-700", label: "No DNS record yet" },
    wrong_target: { pill: "bg-rose-50 text-rose-700", label: "Points somewhere else" },
    lookup_error: { pill: "bg-slate-100 text-slate-600", label: "Lookup failed" },
    no_target: { pill: "bg-slate-100 text-slate-600", label: "Nothing to point at" },
    unset: { pill: "bg-slate-100 text-slate-600", label: "Shared host" },
};

export default function FormsDomainCard() {
    const write = useWriteGuard("MANAGE_SETTINGS");
    const domain = useFormsDomain();
    const save = useSetFormsDomain();
    const verify = useVerifyFormsDomain();
    return (
        <CustomDomainPanel
            label="Forms domain"
            placeholder="forms.yourdomain.com"
            links="form links"
            status={domain.data}
            canWrite={write.allowed}
            guard={(fn) => write.guard(fn)({})}
            saving={save.isPending}
            verifying={verify.isPending}
            onSave={(v) => save.mutateAsync(v)}
            onVerify={() => verify.mutateAsync()}
        />
    );
}

export function CustomDomainPanel({
    label,
    placeholder,
    links,
    status,
    canWrite,
    guard,
    saving,
    verifying,
    onSave: save,
    onVerify: verify,
    target = { name: "forms host", setting: "FORMS_DOMAIN" },
}: {
    label: string;
    placeholder: string;
    /** What the domain is built into, in a sentence: "form links", "client links". */
    links: string;
    /** The shared host a record points at, and the setting an operator sets when it is missing. */
    target?: { name: string; setting: string };
    status: FormsDomainStatus | undefined;
    canWrite: boolean;
    guard: (fn: () => void) => void;
    saving: boolean;
    verifying: boolean;
    onSave: (domain: string) => Promise<FormsDomainStatus>;
    onVerify: () => Promise<FormsDomainStatus>;
}) {
    const [value, setValue] = React.useState("");
    const [touched, setTouched] = React.useState(false);

    // Server state seeds the field until the user starts editing.
    React.useEffect(() => {
        if (!touched && status) setValue(status.forms_domain);
    }, [status, touched]);

    const tone = TONE[status?.status ?? "unset"] ?? TONE.unset;
    const busy = saving || verifying;
    const dirty = touched && value.trim() !== (status?.forms_domain ?? "");

    async function onSave() {
        try {
            const res = await save(value.trim());
            setTouched(false);
            toast.success(
                res.forms_domain === ""
                    ? `Custom domain removed; ${links} use the shared host`
                    : res.forms_domain_verified
                      ? `Verified. New ${links} use your domain.`
                      : "Saved. It will be used as soon as the record resolves.",
            );
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function onVerify() {
        try {
            const res = await verify();
            if (res.forms_domain_verified) toast.success(`Verified. New ${links} use your domain.`);
            else toast(res.message, { icon: "⚠️" });
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function copy(text: string | undefined, what: string) {
        if (!text) return;
        await navigator.clipboard.writeText(text);
        toast.success(`${what} copied`);
    }

    return (
        <div className="grid gap-x-8 gap-y-3 lg:grid-cols-2 items-start">
            <div>
                <div className="flex items-center gap-2">
                    <Label>{label}</Label>
                    <span
                        className={`inline-flex items-center h-4 px-1.5 rounded text-[10px] font-medium mb-1 ${tone.pill}`}
                    >
                        {tone.label}
                    </span>
                </div>
                <div className="flex items-center gap-1.5">
                    <TextInput
                        value={value}
                        onChange={(v) => {
                            setValue(v);
                            setTouched(true);
                        }}
                        placeholder={placeholder}
                        className="flex-1 font-mono"
                        disabled={!canWrite || busy}
                    />
                    <button
                        type="button"
                        disabled={!canWrite || busy || (!dirty && !!status?.forms_domain === false)}
                        onClick={() => guard(() => void onSave())}
                        className="h-7 px-2.5 rounded-md bg-sky-600 text-white text-[12px] font-medium hover:bg-sky-700 disabled:opacity-50 shrink-0"
                    >
                        {saving ? <Loader2Icon className="w-3 h-3 animate-spin" /> : dirty ? "Save and verify" : "Save"}
                    </button>
                </div>
            </div>

            {status?.cname_target ? (
                <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">
                        Add this record
                    </div>
                    <div className="flex items-center gap-2 text-[11.5px] font-mono text-slate-700">
                        <span className="text-slate-400">CNAME</span>
                        <span className="truncate">{value.trim() || placeholder}</span>
                        <span className="text-slate-400">to</span>
                        <span className="truncate">{status.cname_target}</span>
                        <button
                            type="button"
                            onClick={() => void copy(status.cname_target, "CNAME target")}
                            aria-label="Copy CNAME target"
                            className="ml-auto size-6 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-900 hover:bg-slate-200/60 shrink-0"
                        >
                            <CopyIcon className="w-3 h-3" />
                        </button>
                    </div>
                    {status.txt_name && status.txt_value && !status.forms_domain_verified && (
                        <div className="mt-1 flex items-center gap-2 text-[11.5px] font-mono text-slate-700">
                            <span className="text-slate-400">TXT</span>
                            <span className="truncate">{status.txt_name}</span>
                            <span className="text-slate-400">value</span>
                            <span className="truncate">{status.txt_value}</span>
                            <button
                                type="button"
                                onClick={() => void copy(status.txt_value, "TXT value")}
                                aria-label="Copy TXT value"
                                className="ml-auto size-6 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-900 hover:bg-slate-200/60 shrink-0"
                            >
                                <CopyIcon className="w-3 h-3" />
                            </button>
                        </div>
                    )}
                    {status.edge_records?.map((r) => (
                        <div key={r.name} className="mt-1 flex items-center gap-2 text-[11.5px] font-mono text-slate-700">
                            <span className="text-slate-400">{r.type}</span>
                            <span className="truncate">{r.name}</span>
                            <span className="text-slate-400">value</span>
                            <span className="truncate">{r.value}</span>
                            <button
                                type="button"
                                onClick={() => void copy(r.value, "TXT value")}
                                aria-label="Copy activation TXT value"
                                className="ml-auto size-6 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-900 hover:bg-slate-200/60 shrink-0"
                            >
                                <CopyIcon className="w-3 h-3" />
                            </button>
                        </div>
                    ))}
                    {!!status.edge_records?.length && (
                        <p className="mt-1 text-[11px] text-slate-500 font-sans">
                            The activation record is only needed while it is listed. HTTPS starts once it is in place.
                        </p>
                    )}
                </div>
            ) : (
                <p className="text-[11.5px] text-slate-500 rounded-md bg-slate-50 border border-slate-200 px-3 py-2">
                    This install has no {target.name} configured, so there is nothing to point a record at. An operator
                    needs to set {target.setting}.
                </p>
            )}

            {status && status.status !== "unset" && (
                <div className="lg:col-span-2 flex items-start justify-between gap-2">
                    <p className="text-[11.5px] text-slate-500">
                        {status.message}
                        {status.observed ? (
                            <>
                                {" "}
                                Found <span className="font-mono text-slate-700">{status.observed}</span>.
                            </>
                        ) : null}
                    </p>
                    {status.forms_domain && (
                        <button
                            type="button"
                            disabled={!canWrite || busy}
                            onClick={() => guard(() => void onVerify())}
                            className="h-6 px-2 rounded-md border border-slate-200 text-[11px] text-slate-600 hover:bg-slate-50 disabled:opacity-50 shrink-0 inline-flex items-center gap-1"
                        >
                            {verifying ? (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            ) : status.forms_domain_verified ? (
                                <CheckIcon className="w-3 h-3 text-emerald-600" />
                            ) : null}
                            Check again
                        </button>
                    )}
                </div>
            )}

            <p className="lg:col-span-2 text-[11px] text-slate-400">
                Until it verifies, links keep working on the shared host. Warmbly re-checks hourly, so a record that
                finishes propagating starts being used on its own, and one that stops pointing here stops being used
                instead of quietly breaking links.
            </p>
        </div>
    );
}
