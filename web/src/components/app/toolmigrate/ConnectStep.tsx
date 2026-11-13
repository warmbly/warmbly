// Step two: the tool's API key, or one per workspace when the tool issues them that way.

import { AlertCircleIcon, ExternalLinkIcon, PlusIcon, XIcon } from "lucide-react";
import { Label, TextInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import type { ToolCredentials, ToolProviderField, ToolProviderInfo } from "@/lib/api/models/app/toolmigrate/ToolMigration";
import { ToolGlyph } from "./parts";

export default function ConnectStep({
    info,
    creds,
    onChange,
    onAdd,
    onRemove,
    error,
    agency,
}: {
    info: ToolProviderInfo;
    creds: ToolCredentials[];
    onChange: (index: number, key: string, value: string) => void;
    onAdd: () => void;
    onRemove: (index: number) => void;
    error: string | null;
    agency: boolean;
}) {
    const many = creds.length > 1;
    return (
        <div className="p-4 sm:p-5 space-y-4">
            <div className="flex items-start gap-3">
                <ToolGlyph provider={info.id} label={info.label} size={9} />
                <div className="min-w-0">
                    <h3 className="text-[13px] font-semibold text-slate-900">Connect {info.label}</h3>
                    <p className="text-[12px] text-slate-500 mt-0.5 leading-relaxed">{info.key_help}</p>
                    {info.key_url && (
                        <a
                            href={info.key_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="mt-1 inline-flex items-center gap-1 text-[12px] text-sky-700 hover:text-sky-800 hover:underline underline-offset-2"
                        >
                            Open {info.label} to find it
                            <ExternalLinkIcon className="w-3 h-3" />
                        </a>
                    )}
                </div>
            </div>

            <div className="space-y-3">
                {creds.map((c, i) => (
                    <div key={i} className={many ? "rounded-md border border-slate-200 p-3 space-y-3" : "space-y-3"}>
                        {many && (
                            <div className="flex items-center gap-2">
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Workspace key {i + 1}</span>
                                <button
                                    type="button"
                                    onClick={() => onRemove(i)}
                                    aria-label={`Remove key ${i + 1}`}
                                    className="ml-auto size-6 rounded text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center"
                                >
                                    <XIcon className="w-3 h-3" />
                                </button>
                            </div>
                        )}
                        {info.fields.map((f) => (
                            <CredentialField key={f.key} field={f} id={`tm-${i}-${f.key}`} value={c[f.key] ?? ""} onChange={(v) => onChange(i, f.key, v)} autoFocus={i === creds.length - 1 && f === info.fields[0]} />
                        ))}
                    </div>
                ))}
                {info.multi_key && (
                    <div>
                        <button
                            type="button"
                            onClick={onAdd}
                            className="h-7 px-2.5 rounded-md border border-dashed border-slate-300 hover:border-slate-400 text-[12px] text-slate-700 inline-flex items-center gap-1.5 transition-colors"
                        >
                            <PlusIcon className="w-3 h-3" />
                            Add another workspace key
                        </button>
                        <p className="text-[11.5px] text-slate-500 mt-1.5 leading-relaxed">
                            {info.label} issues one key per workspace.{" "}
                            {agency ? "Add one for each client you are moving, and map each to a client next." : "Add one for each workspace you want brought into this one."}
                        </p>
                    </div>
                )}
            </div>

            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                Warmbly only reads from {info.label}. The key is kept encrypted while the migration runs and deleted when it finishes or is canceled.
            </p>

            {error && (
                <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-[12px] text-red-700 flex items-start gap-2">
                    <AlertCircleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                    <span className="min-w-0">{error}</span>
                </div>
            )}
        </div>
    );
}

function CredentialField({
    field,
    id,
    value,
    onChange,
    autoFocus,
}: {
    field: ToolProviderField;
    id: string;
    value: string;
    onChange: (v: string) => void;
    autoFocus?: boolean;
}) {
    return (
        <div>
            <Label htmlFor={id}>
                {field.label}
                {field.optional && <span className="normal-case tracking-normal text-slate-400"> (optional)</span>}
            </Label>
            {field.options?.length ? (
                <SelectMenu
                    value={value}
                    onChange={onChange}
                    options={[...(field.optional ? [{ value: "", label: "Default" }] : []), ...field.options.map((o) => ({ value: o, label: o }))]}
                    fullWidth
                    aria-label={field.label}
                />
            ) : (
                <TextInput
                    id={id}
                    value={value}
                    onChange={onChange}
                    type={field.secret ? "password" : "text"}
                    autoComplete="off"
                    placeholder={field.placeholder}
                    autoFocus={autoFocus}
                    className="w-full font-mono"
                />
            )}
            {field.help && <p className="text-[11.5px] text-slate-500 mt-1 leading-relaxed">{field.help}</p>}
        </div>
    );
}
