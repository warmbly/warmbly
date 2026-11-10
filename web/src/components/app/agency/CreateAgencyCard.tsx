import React from "react";
import { Link } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { FieldError, Label, TextInput } from "@/components/ui/field";
import { useCreateAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { WORKSPACE_NAME_MAX, nameError, normalizeName } from "@/lib/displayName";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function CreateAgencyCard({ onCreated }: { onCreated?: () => void }) {
    const [name, setName] = React.useState("");
    const create = useCreateAgency();
    const error = nameError("Agency name", name, "workspace");

    async function submit() {
        if (error) {
            toast.error(error);
            return;
        }
        try {
            await toast.promise(create.mutateAsync(normalizeName(name)), {
                loading: "Creating agency…",
                success: "Agency created",
                error: (e: AppError) => buildError(e),
            });
            onCreated?.();
        } catch {
            /* surfaced by toast.promise */
        }
    }

    return (
        <div className="max-w-[520px] mx-auto px-5 py-12">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Agency</div>
            <h1 className="mt-1.5 text-[20px] font-semibold text-slate-900 tracking-tight">Run outreach for your clients</h1>
            <p className="mt-2 text-[12.5px] text-slate-500 leading-relaxed">
                An agency keeps every client in its own workspace, under one plan. Clients share one monthly send pool, so
                a quiet week for one client is volume another can use. There is no per-client fee, and your staff get access
                to every client automatically.
            </p>
            <form
                className="mt-6 space-y-3"
                onSubmit={(e) => {
                    e.preventDefault();
                    void submit();
                }}
            >
                <div>
                    <Label>Agency name</Label>
                    <TextInput
                        value={name}
                        onChange={setName}
                        placeholder="Northwind Growth"
                        autoFocus
                        maxLength={WORKSPACE_NAME_MAX}
                        invalid={!!name.trim() && !!error}
                        className="w-full"
                    />
                    <FieldError message={name.trim() ? error : null} />
                </div>
                <div className="flex items-center gap-2 pt-1">
                    <button
                        type="submit"
                        disabled={create.isPending || !!error}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {create.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Create agency
                    </button>
                    <Link to="/app/emails" className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center">
                        Not now
                    </Link>
                </div>
            </form>
        </div>
    );
}
