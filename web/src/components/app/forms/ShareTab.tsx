// ShareTab — hosted link and embed snippets on the left; campaign merge tag
// and per-contact personalized links on the right.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseSearchSchema } from "@/lib/browse-other-lists";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { CheckIcon, CopyIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { SearchInput } from "@/components/ui/field";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { getFormContactLink } from "@/lib/api/client/app/forms";
import type { AppError } from "@/lib/api/client/normalizeError";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type Form from "@/lib/api/models/app/forms/Form";
import buildError from "@/lib/helper/buildError";
import { buildFormLinkToken } from "@/lib/templateVars";

import FormsDomainCard from "./FormsDomainCard";
import Section from "./SettingsSection";

function Snippet({ label, hint, code }: { label?: string; hint?: string; code: string }) {
    const [copied, setCopied] = React.useState(false);
    async function copy() {
        await navigator.clipboard.writeText(code);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
    }
    return (
        <div>
            {(label || hint) && (
                <div className="flex items-baseline gap-2 mb-1">
                    {label && <span className="text-[12.5px] font-medium text-slate-900">{label}</span>}
                    {hint && <span className="text-[11px] text-slate-500">{hint}</span>}
                </div>
            )}
            <div className="relative">
                <pre className="rounded-md border border-slate-200 bg-slate-50 p-3 pr-16 text-[11.5px] leading-relaxed text-slate-700 overflow-x-auto whitespace-pre-wrap break-all">
                    {code}
                </pre>
                <button
                    type="button"
                    onClick={() => void copy()}
                    className="absolute top-2 right-2 inline-flex items-center gap-1 h-6 px-2 rounded-md border border-slate-200 bg-white text-[11px] text-slate-600 hover:bg-slate-50"
                >
                    {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                    {copied ? "Copied" : "Copy"}
                </button>
            </div>
        </div>
    );
}

function contactName(c: Contact): string {
    return [c.first_name, c.last_name].filter(Boolean).join(" ") || c.email;
}

function ContactLinkRow({ form, contact }: { form: Form; contact: Contact }) {
    const [state, setState] = React.useState<"idle" | "loading" | "copied">("idle");
    const name = contactName(contact);

    async function copyLink() {
        setState("loading");
        try {
            const res = await getFormContactLink(form.id, contact.id);
            await navigator.clipboard.writeText(res.url);
            setState("copied");
            setTimeout(() => setState("idle"), 1500);
        } catch (err) {
            setState("idle");
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <div className="h-9 px-2 flex items-center gap-2 rounded-md hover:bg-slate-50">
            <div className="flex-1 min-w-0 leading-tight">
                <div className="text-[12px] text-slate-900 truncate">{name}</div>
                {name !== contact.email && <div className="text-[10.5px] text-slate-500 truncate">{contact.email}</div>}
            </div>
            <button
                type="button"
                onClick={() => void copyLink()}
                disabled={state === "loading"}
                className="shrink-0 inline-flex items-center gap-1 h-6 px-2 rounded-md border border-slate-200 bg-white text-[11px] text-slate-600 hover:bg-slate-50 disabled:opacity-60"
            >
                {state === "copied" ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                {state === "copied" ? "Copied" : state === "loading" ? "Copying…" : "Copy link"}
            </button>
        </div>
    );
}

function PersonalizedLinksCard({ form }: { form: Form }) {
    const [query, setQuery] = useBrowseState(`forms.${form.id}.share.contact-search`, "", browseSearchSchema);
    const debouncedQuery = useDebouncedValue(query);
    const active = debouncedQuery.trim().length >= 2;

    const search = useSearchContacts({
        options: {
            query: debouncedQuery.trim(),
            custom_field_filters: [],
            campaign_ids: [],
            sort_by: "updated_at",
            reverse: false,
        },
        limit: 6,
        enabled: active,
        keepPrevious: true,
    });
    const contacts = search.contacts ?? [];

    if (form.status !== "published") {
        return (
            <p className="text-[11.5px] text-slate-500 rounded-md bg-slate-50 border border-slate-200 px-3 py-2">
                Personalized links go live when the form is published. Publish it, then come back here to grab the
                campaign tag or copy a contact's personal link.
            </p>
        );
    }

    return (
        <div className="grid gap-x-8 gap-y-5 lg:grid-cols-2">
            <Snippet
                label="Use in a campaign email"
                hint="each recipient gets their own link at send time"
                code={buildFormLinkToken(form.public_id)}
            />

            <div>
                <div className="text-[12.5px] font-medium text-slate-900 mb-1">Copy a link for one contact</div>
                <SearchInput value={query} onChange={setQuery} placeholder="Search contacts by name or email…" />
                {active && (
                    <div className="mt-1.5 flex flex-col">
                        {contacts.map((c) => (
                            <ContactLinkRow key={c.id} form={form} contact={c} />
                        ))}
                        {contacts.length === 0 && !search.isFetching && (
                            <p className="text-[11.5px] text-slate-500 px-2 py-1.5">No contacts match "{debouncedQuery.trim()}".</p>
                        )}
                        {contacts.length === 0 && search.isFetching && (
                            <p className="text-[11.5px] text-slate-400 px-2 py-1.5">Searching…</p>
                        )}
                    </div>
                )}
            </div>
        </div>
    );
}

export default function ShareTab({ form, baseUrl }: { form: Form; baseUrl: string }) {
    const userID = React.useContext(UserContext)?.user.id;
    const organizationID = useAppStore((state) => state.currentOrganization?.id ?? "personal");
    const pageUrl = form.share_url || (baseUrl ? `${baseUrl}/f/${form.public_id}` : "");
    // The embed loads its iframe from its own origin, so the snippet has to
    // come from the same host as the page: an organization on a verified
    // custom forms domain would otherwise embed on the shared one.
    const scriptOrigin = React.useMemo(() => {
        try {
            return new URL(pageUrl).origin;
        } catch {
            return baseUrl;
        }
    }, [pageUrl, baseUrl]);
    const scriptUrl = scriptOrigin ? `${scriptOrigin}/forms.js` : "";

    if (!pageUrl) {
        return (
            <div className="p-6 text-[12.5px] text-slate-500">
                No public URL is configured for this instance. Set API_PUBLIC_URL (or FORMS_DOMAIN) on the backend.
            </div>
        );
    }

    return (
        <div className="px-4 lg:px-6 py-5">
            {form.status !== "published" && (
                <div className="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] text-amber-800">
                    This form is not published yet. The link and embeds below go live the moment you publish.
                </div>
            )}

            <div className="divide-y divide-slate-200/60">
                <Section title="Hosted link" description="The form's own page. Share it anywhere, no website needed.">
                    <div className="col-span-full max-w-3xl">
                        <Snippet code={pageUrl} />
                    </div>
                </Section>

                <Section
                    title="Embed on your site"
                    description="These work in WordPress, Webflow, Shopify, Framer and any site that accepts custom HTML."
                >
                    <Snippet
                        label="JavaScript embed"
                        hint="recommended, self-sizing"
                        code={`<script src="${scriptUrl}" async></script>\n<div data-warmbly-form="${form.public_id}"></div>`}
                    />
                    <Snippet
                        label="Popup"
                        hint="opens in an overlay"
                        code={`<script src="${scriptUrl}" async></script>\n<button data-warmbly-popup="${form.public_id}">Get started</button>`}
                    />
                    <Snippet
                        label="Plain iframe"
                        hint="for builders that strip scripts"
                        code={`<iframe src="${pageUrl}?embed=1" width="100%" height="600" style="border:0" title="${form.name.replace(/"/g, "&quot;")}"></iframe>`}
                    />
                    <p className="col-span-full text-[11.5px] text-slate-500">
                        {form.allowed_domains.length > 0
                            ? `Embedding is limited to: ${form.allowed_domains.join(", ")}.`
                            : "Restrict which sites may embed this form from the Settings tab."}
                    </p>
                </Section>

                <Section
                    title="Personalized links"
                    description="Each contact gets their own link. Opening it prefills the form and ties the submission and every page view back to that contact and the campaign that sent it."
                >
                    <div className="col-span-full">
                        <PersonalizedLinksCard key={`${userID}:${organizationID}:${form.id}`} form={form} />
                    </div>
                </Section>

                <Section
                    title="Custom domain"
                    description="Form links go out on a shared host by default. Point a subdomain of the domain you send from at it and every link, including the personalized ones in campaign emails, carries your own name."
                >
                    <div className="col-span-full">
                        <FormsDomainCard />
                    </div>
                </Section>
            </div>
        </div>
    );
}
