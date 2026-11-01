import React from "react";
import { Loader2Icon, SparklesIcon, UserRoundIcon } from "lucide-react";
import { FieldError, SearchInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger, SelectButton } from "@/components/ui/popover-menu";
import { OptionSelect } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { contactLabel } from "@/components/app/campaigns/sequences/previewContext";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import { useCompanyEnrichContact } from "@/lib/api/hooks/app/integrations/useCompanyEnrich";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type { CompanyEnrichContactResult } from "@/lib/api/models/app/integrations/CompanyEnrich";
import { primaryBtn } from "../salesforce/_components/shared";
import { errMsg } from "../salesforce/_components/util";
import Results from "./Results";

export default function ContactEnrichment({ connectionId, enabled }: { connectionId: string; enabled: boolean }) {
    const [contact, setContact] = React.useState<Contact | null>(null);
    const [kind, setKind] = React.useState<"person" | "company">("person");
    const [open, setOpen] = React.useState(false);
    const [searchText, setSearchText] = React.useState("");
    const [result, setResult] = React.useState<CompanyEnrichContactResult | null>(null);
    const [error, setError] = React.useState("");
    const generation = React.useRef(0);
    const inFlight = React.useRef(false);
    const query = useDebouncedValue(searchText.trim(), 250);
    const search = useSearchContacts({ options: { query, custom_field_filters: [], campaign_ids: [], sort_by: "updated_at", reverse: false }, limit: 8, enabled: enabled && open });
    const enrich = useCompanyEnrichContact(connectionId);
    React.useEffect(() => () => { generation.current += 1; }, []);

    function reset() { generation.current += 1; setResult(null); setError(""); }
    async function run() {
        if (!contact || !enabled || inFlight.current) return;
        const stamp = generation.current;
        inFlight.current = true; setOpen(false); setError(""); setResult(null);
        try {
            const response = await enrich.mutateAsync({ contact_id: contact.id, kind, overwrite: false });
            if (stamp === generation.current) { setResult(response); setContact(response.contact); }
        } catch (reason) { if (stamp === generation.current) setError(errMsg(reason, "The contact could not be enriched.")); }
        finally { inFlight.current = false; }
    }

    return <section className="space-y-3">
        <h2 className="text-[14px] font-semibold text-slate-900">Enrich an existing contact</h2>
        <p className="max-w-2xl text-[12.5px] text-slate-500">Fill empty fields using the contact’s saved email or company website. Existing values, email addresses, subscriptions and campaign membership stay unchanged.</p>
        <div className="space-y-4 rounded-md border border-slate-200 p-4 sm:p-5">
            <fieldset disabled={!enabled || enrich.isPending} className="space-y-4 disabled:opacity-60">
                <div className="max-w-md">
                    <p className="mb-1.5 text-[12px] font-medium text-slate-700">Warmbly contact</p>
                    <PopoverMenu open={open} onOpenChange={setOpen}>
                        <PopoverMenuTrigger asChild><SelectButton disabled={!enabled || enrich.isPending} aria-label="Choose an existing contact" icon={<UserRoundIcon className="size-3.5" />} label={contact ? `${contactLabel(contact)} · ${contact.email}` : "Choose an existing contact"} className="w-full" /></PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={280} matchTriggerWidth className="p-1">
                            <div className="p-1.5"><SearchInput value={searchText} onChange={setSearchText} placeholder="Search existing contacts…" autoFocus /></div>
                            <div className="max-h-56 overflow-y-auto">
                                {search.isError ? <p className="px-3 py-2 text-[12px] text-red-600">Contacts could not be loaded.</p> : search.isLoading ? <p className="px-3 py-2 text-[12px] text-slate-500">Loading contacts…</p> : (search.contacts ?? []).length === 0 ? <p className="px-3 py-2 text-[12px] text-slate-500">No contacts found.</p> : (search.contacts ?? []).map((item) => <PopoverMenuItem key={item.id} selected={contact?.id === item.id} onSelect={() => { reset(); setContact(item); setOpen(false); }}><span>{contactLabel(item)}</span><span className="ml-2 text-slate-400">{item.email}</span></PopoverMenuItem>)}
                            </div>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
                <OptionSelect value={kind} onChange={(value) => { reset(); setKind(value); }} cols={2} aria-label="Contact enrichment type" options={[{ value: "person", label: "Person details", hint: "Match the contact’s saved email." }, { value: "company", label: "Company details", hint: "Match the contact’s saved company website or work email domain." }]} />
                <p className="rounded-md border border-amber-200 bg-amber-50 p-3 text-[12px] text-amber-900">Enriching a contact makes one request using your CompanyEnrich credits and fills empty fields only.</p>
                <button type="button" onClick={() => void run()} className={primaryBtn} disabled={!enabled || !contact || enrich.isPending}>{enrich.isPending ? <Loader2Icon className="size-3.5 animate-spin" /> : <SparklesIcon className="size-3.5" />}{enrich.isPending ? "Enriching…" : "Enrich contact"}</button>
            </fieldset>
            <FieldError message={error} />
        </div>
        {result && <div className="space-y-3"><p role="status" className="rounded-md border border-sky-100 bg-sky-50 p-3 text-[12.5px] text-sky-800">{result.updated_fields.length ? `Updated ${contactLabel(result.contact)}: ${result.updated_fields.map((key) => key.replace(/_/g, " ")).join(", ")}.` : `No empty fields could be filled for ${contactLabel(result.contact)}.`}</p><Results result={result} /></div>}
    </section>;
}
