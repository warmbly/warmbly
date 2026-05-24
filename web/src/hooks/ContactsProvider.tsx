"use client";

import { APIError, Call } from "@/lib/api";
import React, { createContext, useContext } from "react";
import { useError } from "./ErrorProvider";
import { Input, TextArea } from "@/components/input";
import { RiAddLine, RiCloseCircleLine, RiCloseLine, RiSearch2Line } from "@remixicon/react";
import { Loading } from "@/components/loader";
import MiniInput from "@/components/app/popup/MiniInput";
import Checkbox from "@/components/app/Checkbox";
import MiniNumberInput from "@/components/app/popup/MiniNumberInput";
import MiniDate from "@/components/app/popup/MiniDate";
import Switch from "@/components/app/Switch";
import Selector from "@/components/app/popup/select/Selector";
import SelectMenu from "@/components/app/popup/select/SelectMenu";
import SelectOption from "@/components/app/popup/select/SelectOption";
import { twColors } from "tailwindv4-colors";
import useClickOutside from "./useClickOutside";
import CampaignSelector from "@/components/app/popup/select/CampaignSelector";
import AddContacts from "@/components/app/AddContacts";
import BulkEditContactsProvider from "./BulkEditContactsProvider";
import MiniTextArea from "@/components/app/popup/MiniTextArea";

export interface ContactCampaign {
    id: string;
    name: string;
}

export interface ContactRaw {
    id: string;

    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;

    custom_fields: Record<string, string>;

    subscribed: boolean;
    campaigns: ContactCampaign[];

    updated_at: string;
    created_at: string;
}

export interface Contact extends Omit<ContactRaw, 'updated_at' | 'created_at'> {
    updatedAt: Date;
    createdAt: Date;
}

export function parseContact(raw: ContactRaw): Contact {
    return {
        ...raw,
        updatedAt: new Date(raw.updated_at),
        createdAt: new Date(raw.created_at),
    }
}

export function parseContacts(raw: ContactRaw[]): Contact[] {
    return raw.map((i) => parseContact(i))
}

export interface AddContacts {
    show_campaigns: boolean;
    campaigns: string[];
}

interface ContactsContextType {
  loading: boolean;
  contacts: Contact[] | null;
  count: number;
  setContacts: React.Dispatch<React.SetStateAction<Contact[] | null>>
  GetContacts: (s?: SearchContacts | null, refresh?: boolean) => Promise<void>;
  SearchContacts: (q: string) => Promise<void>;
  UpdateContact: (c: Contact) => void;
  search: SearchContacts;
  setView: React.Dispatch<React.SetStateAction<string>>;
  setSearch: React.Dispatch<React.SetStateAction<SearchContacts>>;
  setFilters: React.Dispatch<React.SetStateAction<boolean>>;
  showFilters: (campaign?: ContactCampaign) => void;
  add: AddContacts | null;
  setAdd: React.Dispatch<React.SetStateAction<AddContacts | null>>;
  deleteLoad: string;
  DeleteContact: (id: string) => Promise<void>;
  setCount: React.Dispatch<React.SetStateAction<number>>;
}

export const ContactsContext = createContext<ContactsContextType | undefined>(undefined);

function MiniTitle({children}:{children: React.ReactNode}){
    return <h1 className="text-slate-500 font-semibold font-inter mb-3 text-sm uppercase tracking-wider flex items-center gap-2">{children}</h1>
}

type SortBy = 'created_at' | 'updated_at' | 'first_name' | 'last_name' | 'email' | 'campaign_count';

const SortByNames: Record<SortBy, string> = {
    'created_at': "Created At",
    'updated_at': "Updated At",
    'first_name': "First Name",
    'last_name': "Last Name",
    'email': "Email",
    'campaign_count': "Campaign Count"
}

type CustomFieldFilterType = 'equal' | 'starts_with' | 'ends_with' | 'contains';

const CustomFieldFilterTypeNames: Record<CustomFieldFilterType, string> = {
    'equal': "Equal",
    'starts_with': "Starts With",
    'ends_with': "Ends With",
    'contains': "Contains"
}

export interface CustomFieldFilter {
    name: string;
    value: string;
    type: CustomFieldFilterType;
}

export interface SearchContacts {
    query: string;
    custom_field_filters: CustomFieldFilter[],
    campaigns: ContactCampaign[],
    min_campaigns: number | null,
    max_campaigns: number | null,
    subscribed: boolean | null,
    created_after: Date | null,
    created_before: Date | null,
    updated_after: Date | null,
    updated_before: Date | null,
    sort_by: SortBy,
    reverse: boolean
}

export interface CustomField {
    name: string;
    value: string;
}

export function CheckLine({
    children,
    value,
    setValue,
}:{
    children: React.ReactNode,
    value: boolean,
    setValue: (v: boolean) => void,
}){
    return (
        <div className="flex">
            <label
            className="flex items-center cursor-pointer select-none text-slate-600 gap-4"
            >
                <input
                    type="checkbox"
                    checked={value}
                    onChange={() => setValue(!value)}
                    style={{ position: 'absolute', opacity: 0 }}
                />
                <Checkbox checked={value} />
                <span className="text-lg">{children}</span>
            </label>
        </div>
    )
}

export function CheckFilter({
    children,
    label,
    value,
    setValue
}:{
    children?: React.ReactNode,
    label: string,
    value: boolean,
    setValue: (v: boolean) => void,
}){
    return <div>
        <CheckLine
         value={value}
         setValue={setValue}>
            {label}
        </CheckLine>
        {(value && children) && (
            <div className="flex">
                <div className="w-5 shrink-0 flex justify-center py-2">
                    <div className="w-px h-full bg-slate-300"/>
                </div>
                <div className="flex-1 px-4 py-3">
                    {children}
                </div>
            </div>
        )}
    </div>
}

export function CheckFilterTime({
    label,
    value,
    setValue
}:{
    label: string,
    value: Date | null,
    setValue: (v: Date | null) => void,
}){
    return <CheckFilter
        value={value !== null}
        setValue={(v) => {
            if(v) {
                setValue(new Date())
            } else {
                setValue(null)
            }
        }}
        label={label}
    >
        <MiniDate
            onChange={(v) => setValue(v ?? null)}
            placeholder="Null"
            value={value ?? undefined}
        />
    </CheckFilter>
}

export const ContactsProvider = ({ children }: { children: React.ReactNode }) => {
    const { showError } = useError();

    const [contacts, setContacts] = React.useState<Contact[] | null>(null);
    const [search, setSearch] = React.useState<SearchContacts>({
        query: "",
        custom_field_filters: [],
        campaigns: [],
        min_campaigns: null,
        max_campaigns: null,
        subscribed: null,
        created_after: null,
        created_before: null,
        updated_after: null,
        updated_before: null,
        sort_by: 'created_at',
        reverse: false
    })
    const [add, setAdd] = React.useState<AddContacts | null>(null);
    const [name, setName] = React.useState<string>("");
    const [description, setDescription] = React.useState<string>("")
    const [newLoad, setNewLoad] = React.useState<boolean>(false);
    const [error, setError] = React.useState<string>("");
    const [loading, setLoading] = React.useState<boolean>(false);
    const [count, setCount] = React.useState<number>(0)
    const [view, setView] = React.useState<string>("");
    const [filters, setFilters] = React.useState<boolean>(false);
    const [deleteLoad, setDeleteLoad] = React.useState<string>("");
    const [activeCampaign, setActiveCampaign] = React.useState<ContactCampaign | null>(null);

    function showFilters(campaign?: ContactCampaign){
        if (campaign) {
            setActiveCampaign(campaign);
        } else {
            setActiveCampaign(null)
        };
        setFilters(true);
    }

    const GetContacts = async (s?: SearchContacts | null, refresh?: boolean) => {
        if (loading) return;
        if (count === contacts?.length && s === undefined && !refresh) return;
        try {
            setLoading(true);
            if (s !== undefined){
                setContacts(null)
                if (s !== null) setSearch(s);
            } else if (refresh){
                setContacts(null);
            }

            const offset = (contacts && !refresh) ? contacts.length:0

            let data;
            if (s) {
                data = {
                    ...s,
                    campaign_ids: s.campaigns.map((c) => c.id),
                    offset,
                }
            } else {
                data = {
                    ...search,
                    campaign_ids: search.campaigns.map((c) => c.id),
                    offset,
                }
            }

            const body = await Call(
                `/contacts/search`, 
                "POST", 
                data,
            );
            if (offset === 0) {
                setCount(body.count)
                const c: ContactRaw[] = body.data;
                setContacts(parseContacts(c))
            } else {
                const c: ContactRaw[] = body
                const contacts = parseContacts(c)
                setContacts(bef => bef ? [...bef, ...contacts]:contacts)
            }
        } catch (err) {
            if (err instanceof APIError) {
                showError(err.message, err.body.message)
            } else {
                showError("Client Error", `${err}`)
            }
        } finally {
            setLoading(false);
        }
    }

    const SearchContacts = async (q: string, campaign?: ContactCampaign) => {
        return await GetContacts({
            ...search,
            campaigns: campaign ? [campaign]:search.campaigns,
            query: q,
        })
    }

    const UpdateContact = (contact: Contact) => {
        setContacts(bef => bef ? bef.map((c) => c.id === contact.id ? contact:c):null)
    }

    const NewContact = async () => {
        if (!newLoad){
            try {
                setNewLoad(true)
                const contact: ContactRaw = await Call(`/contacts`, "POST", {
                    name,
                    description,
                })
                setContacts(bef => bef ? [parseContact(contact), ...bef]:[parseContact(contact)])
                setAdd(null);
                setError("");
            } catch (err) {
                if (err instanceof APIError){
                    setError(`${err.message}: ${err.body.message}`)
                } else {
                    setError(`Client Error: ${err}`)
                }
            } finally {
                setNewLoad(false)
            }
        }
    }

    async function DeleteContact(id: string) {
        if (deleteLoad !== "") return;
        try {
            setDeleteLoad(id);
            await Call(`/contacts/${id}`, "DELETE", undefined, true)
            setContacts(bef => bef ? bef.filter(c => c.id !== id):null)
            setCount(bef => bef-1)
        } catch (err) {
            if (err instanceof APIError) {
                showError(err.message, err.body.message)
            } else {
                showError("Client Error", String(err))
            }
        } finally {
            setDeleteLoad("");
        }
    }


    const [mouseDownOnButton, setMouseDownOnButton] = React.useState(false);
    const handleMouseDown = () => setMouseDownOnButton(true);
    const handleMouseUp = (set: React.Dispatch<React.SetStateAction<boolean>> | React.Dispatch<React.SetStateAction<string>>) => {
        if (mouseDownOnButton) {
            if (typeof set === "function") {
                (set as any)((prev: any) => (typeof prev === "string" ? "" : false));
            }
        }
        setMouseDownOnButton(false);
    };

    return (
        <ContactsContext.Provider value={{ contacts, showFilters, setContacts, SearchContacts, DeleteContact, deleteLoad, loading, count, setView, GetContacts, UpdateContact, search, setSearch, setFilters, add, setAdd, setCount}}>
            <BulkEditContactsProvider>
                {children}
            </BulkEditContactsProvider>
            <div onMouseDown={handleMouseDown} onMouseUp={() => handleMouseUp(setFilters)} className={`fixed inset-0 z-100 flex justify-end bg-slate-950/45 transition ${filters ? "visible opacity-100":"invisible opacity-0"}`} >
                <div onMouseDown={(e) => e.stopPropagation()} onMouseUp={(e) => e.stopPropagation()} className={`flex flex-col bg-white relative w-200 max-w-[95%] h-full transition-transform ${filters ? "translate-x-0":"translate-x-100"}`}>
                    <div className="overflow-y-scroll p-10 grow">
                        <div className="mb-3 flex justify-between gap-5">
                            <h1 className="text-3xl text-slate-700 font-poppins font-medium">Search Filters</h1>
                            <div onClick={() => setFilters(false)} className="flex px-2 items-center justify-center hover:opacity-80 cursor-pointer">
                                <RiCloseLine className="w-5 text-slate-400"/>
                            </div>
                        </div>
                        <hr className="my-5 text-slate-200"/>
                        <div className="space-y-5">
                            <div>
                                <MiniTitle>Query</MiniTitle>
                                <MiniInput
                                    value={search.query}
                                    placeholder="Search..."
                                    onChange={(e) => setSearch(s => ({
                                        ...s,
                                        query: e.target.value,
                                    }))}
                                />
                            </div>
                            <div>
                                <MiniTitle>
                                    Custom Field Filters
                                    {search.custom_field_filters.length < 100 &&
                                    <button 
                                    onClick={() => {
                                        setSearch(prev =>
                                            ({
                                                ...prev,
                                                custom_field_filters: [...prev.custom_field_filters, {
                                                    name: "",
                                                    type: "contains",
                                                    value: "",
                                                }]
                                            })
                                        )
                                    }}
                                    className='ripple bg-blue-100 hover:bg-blue-200 transition rounded-lg px-1 text-blue-600 cursor-pointer'>
                                        <RiAddLine className='w-4'/>
                                    </button>}
                                </MiniTitle>
                                <div className="space-y-3">
                                {search.custom_field_filters.length > 0 ? <>
                                    {search.custom_field_filters.map((f, i) => (
                                        <CustomFieldRow
                                            key={i}
                                            field={f}
                                            onChange={(updated) =>
                                                setSearch(prev =>
                                                    ({
                                                        ...prev,
                                                        custom_field_filters: prev.custom_field_filters.map((item, ind) => (ind === i ? updated : item))
                                                    })
                                                )
                                            }
                                            onDelete={() =>
                                            setSearch((prev) => ({
                                                ...prev,
                                                custom_field_filters: prev.custom_field_filters.filter((_, ind) => ind !== i),
                                            }))
                                            }
                                        />
                                    ))}
                                </>:<>
                                <div className=" text-slate-400 font-poppins">
                                    No filters added yet.
                                </div>
                                </>}
                                </div>
                            </div>
                            <div>
                                <MiniTitle>Sort By</MiniTitle>
                                <SortBySelection
                                    search={search}
                                    setSearch={setSearch}
                                />
                            </div>
                            <div>
                                <MiniTitle>Filter By</MiniTitle>
                                <div className="space-y-3">
                                    <CheckFilterTime
                                        value={search.created_after}
                                        setValue={(v) => {
                                            setSearch(bef => ({...bef, created_after: v}))
                                        }}
                                        label="Created After"
                                    />
                                    <CheckFilterTime
                                        value={search.updated_after}
                                        setValue={(v) => {
                                            setSearch(bef => ({...bef, updated_after: v}))
                                        }}
                                        label="Updated After"
                                    />
                                    <CheckFilterTime
                                        value={search.created_before}
                                        setValue={(v) => {
                                            setSearch(bef => ({...bef, created_before: v}))
                                        }}
                                        label="Created Before"
                                    />
                                    <CheckFilterTime
                                        value={search.updated_before}
                                        setValue={(v) => {
                                            setSearch(bef => ({...bef, updated_before: v}))
                                        }}
                                        label="Updated Before"
                                    />
                                    <CheckFilter
                                        value={search.min_campaigns !== null}
                                        setValue={(v) => {
                                            if (v) {
                                                setSearch(bef => ({...bef, min_campaigns: 0}))
                                            } else {
                                                setSearch(bef => ({...bef, min_campaigns: null}))
                                            }
                                        }}
                                        label="Min Campaigns"
                                    >
                                        {search.min_campaigns !== null &&
                                        <MiniNumberInput
                                            value={search.min_campaigns}
                                            placeholder="0"
                                            onChange={(e) => setSearch(bef => ({...bef, min_campaigns: e.target.valueAsNumber}))}
                                        />}
                                    </CheckFilter>
                                    <CheckFilter
                                        value={search.max_campaigns !== null}
                                        setValue={(v) => {
                                            if (v) {
                                                setSearch(bef => ({...bef, max_campaigns: 0}))
                                            } else {
                                                setSearch(bef => ({...bef, max_campaigns: null}))
                                            }
                                        }}
                                        label="Max Campaigns"
                                    >
                                        {search.max_campaigns !== null &&
                                        <MiniNumberInput
                                            value={search.max_campaigns}
                                            placeholder="0"
                                            onChange={(e) => setSearch(bef => ({...bef, max_campaigns: e.target.valueAsNumber}))}
                                        />}
                                    </CheckFilter>
                                    <CheckFilter
                                        value={search.subscribed !== null}
                                        setValue={(v) => {
                                            if (v) {
                                                setSearch(bef => ({...bef, subscribed: true}))
                                            } else {
                                                setSearch(bef => ({...bef, subscribed: null}))
                                            }
                                        }}
                                        label="Subscribed"
                                    >
                                        {search.subscribed !== null && (
                                            <Switch
                                                id="contact-filter-subscribed"
                                                value={search.subscribed}
                                                onChange={(v) => setSearch(bef => ({...bef, subscribed: v}))}
                                            />
                                        )}
                                    </CheckFilter>
                                </div>
                            </div>
                            {!activeCampaign &&
                            <div>
                                <MiniTitle>Assoicated Campaigns</MiniTitle>
                                <CampaignSelector
                                 onAdd={(id, name) => {
                                    setSearch(bef => ({
                                        ...bef,
                                        campaigns: [...bef.campaigns, {id, name: name ?? ''}]
                                    }))
                                 }}
                                 onRemove={(id) => {
                                    setSearch(bef => ({
                                        ...bef,
                                        campaigns: bef.campaigns.filter((cm) => cm.id !== id)
                                    }))
                                 }}
                                 selected={search.campaigns}
                                 reverse
                                 />
                            </div>}
                            <div className="flex justify-end gap-2 mt-6">
                                <button 
                                 onClick={() => setFilters(false)}
                                 className="ripple cursor-pointer px-4 h-10 text-slate-500 rounded-lg bg-slate-200 hover:bg-slate-300">
                                    Close
                                </button>
                            <button 
                            onClick={async () => {
                                await GetContacts({
                                    ...search,
                                    campaigns: activeCampaign ? [activeCampaign]:search.campaigns
                                })
                            }}
                            className='ripple w-27 h-10 gap-1 cursor-pointer text-white transition flex items-center justify-center bg-blue-500 hover:bg-blue-600 rounded-lg'
                            >
                                {contacts === null ? <Loading className='h-5' color={twColors.slate[200]}/>:<>
                                <RiSearch2Line className="w-4"/>
                                Search
                                </>}
                            </button>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
            {(() => {
                const preview = contacts?.find((e) => e.id === view)
                const [body, setBody] = React.useState<Contact | null>(null)
                const [customFields, setCustomFields] = React.useState<CustomField[]>([])
                const [loading, setLoading] = React.useState<boolean>(false);
                const [error, setError] = React.useState<string>("");

                React.useEffect(() => {
                    ResetBody();
                }, [preview])

                function ResetBody(){
                    setBody(preview ? preview:null)
                    setCustomFields(preview ?  Object.entries(preview.custom_fields).map(([n, v]) => ({
                        name: n,
                        value: v,
                    })):[])
                }

                const [mouseDownOnButton, setMouseDownOnButton] = React.useState(false);
                const handleMouseDown = () => setMouseDownOnButton(true);
                const handleMouseUp = () => {
                    if (mouseDownOnButton) {
                        setView(""); setError("");
                    }
                    setMouseDownOnButton(false);
                };

                function MakeRecordFromCustomFields(custom_fields: CustomField[]){
                    const newData: Record<string, string> = {}
                    customFields.forEach(f => {
                        newData[f.name] = f.value
                    }) 
                    return newData
                }

                const isChangedCustomFields = React.useMemo(() => {
                    const newData = MakeRecordFromCustomFields(customFields)
                    if (JSON.stringify(newData) !== JSON.stringify(preview?.custom_fields)){
                        return true
                    } else {
                        return false
                    }
                }, [customFields, preview])

                const isChangedCampaigns = React.useMemo(() => {
                    if (body?.campaigns.some(c => !preview?.campaigns.some(ca => ca.id === c.id)) || preview?.campaigns.some(c => !body?.campaigns.some(ca => ca.id === c.id))){
                        return true
                    } else {
                        return false
                    }
                }, [preview?.campaigns, body?.campaigns])

                async function SaveChanges(){
                    if (!body || !preview) return;
                    try {
                        setLoading(true)
                        const data = {
                            ...(preview.first_name !== body.first_name && ({first_name: body.first_name})),
                            ...(preview.last_name !== body.last_name && ({last_name: body.last_name})),
                            ...(preview.email !== body.email && ({email: body.email})),
                            ...(preview.company !== body.company && ({company: body.company})),
                            ...(preview.phone !== body.phone && ({phone: body.phone})),
                            ...(preview.subscribed !== body.subscribed && ({subscribed: body.subscribed})),
                            ...(isChangedCustomFields && (MakeRecordFromCustomFields(customFields))),
                            ...(isChangedCampaigns && ({campaigns: body?.campaigns.map(c => c.id)}))
                        }
                        const req: ContactRaw = await Call(`/contacts/${preview.id}`, "PATCH", data)
                        const contact = parseContact(req);
                        setContacts(bef => bef ? bef.map(c => c.id === preview.id ? contact:c):null)
                        setError("");
                    } catch (err) {
                        if (err instanceof APIError) {
                            setError(`${err.message}: ${err.body.message}`)
                        } else {
                            setError(`Client Error: ${err}`)
                        }
                    } finally {
                        setLoading(false)
                    }
                }

                return (
                    <div onMouseDown={handleMouseDown} onMouseUp={handleMouseUp} className={`fixed inset-0 z-100 flex justify-end bg-slate-950/45 transition ${preview ? "visible opacity-100":"invisible opacity-0"}`}>
                        <div onMouseDown={(e) => e.stopPropagation()} onMouseUp={(e) => e.stopPropagation()} className={`flex p-10 flex-col bg-white relative w-200 max-w-[95%] h-full transition-transform overflow-y-scroll ${preview ? "translate-x-0":"translate-x-100"}`}>
                            {(body && preview) && <>
                                <div className="space-y-5">
                                    <div className="flex justify-between gap-4 items-center">
                                        <h1 className="font-semibold font-poppins text-slate-600 text-lg">Edit Contact ({preview.email})</h1>
                                        <button 
                                         className="shrink-0 px-2 cursor-pointer text-slate-400 hover:text-slate-300"
                                         onClick={() => {
                                            setView(""); setError("");
                                         }}>
                                            <RiCloseLine className="w-5 h-5"/>
                                        </button>
                                    </div>
                                    <hr className="text-slate-200"/>
                                    <div>
                                        <MiniTitle>First Name</MiniTitle>
                                        <MiniInput
                                         value={body.first_name}
                                         placeholder="e.g. John"
                                         onChange={(e) => {
                                            setBody(bef => bef ? ({
                                                ...bef,
                                                first_name: e.target.value,
                                            }):null)
                                         }}/>
                                    </div>
                                    <div>
                                        <MiniTitle>Last Name</MiniTitle>
                                        <MiniInput
                                         value={body.last_name}
                                         placeholder="e.g. Doe"
                                         onChange={(e) => {
                                            setBody(bef => bef ? ({
                                                ...bef,
                                                last_name: e.target.value,
                                            }):null)
                                         }}/>
                                    </div>
                                    <div>
                                        <MiniTitle>Email</MiniTitle>
                                        <MiniInput
                                         value={body.email}
                                         placeholder="e.g. name@example.com"
                                         onChange={(e) => {
                                            setBody(bef => bef ? ({
                                                ...bef,
                                                email: e.target.value,
                                            }):null)
                                         }}/>
                                    </div>
                                    <div>
                                        <MiniTitle>Company</MiniTitle>
                                        <MiniInput
                                         value={body.company}
                                         placeholder="e.g. Acme Inc."
                                         onChange={(e) => {
                                            setBody(bef => bef ? ({
                                                ...bef,
                                                company: e.target.value,
                                            }):null)
                                         }}/>
                                    </div>
                                    <div>
                                        <MiniTitle>Phone</MiniTitle>
                                        <MiniInput
                                         value={body.phone}
                                         placeholder="e.g. +1 (123) 456-7890"
                                         onChange={(e) => {
                                            setBody(bef => bef ? ({
                                                ...bef,
                                                phone: e.target.value,
                                            }):null)
                                         }}/>
                                    </div>
                                    <div>
                                        <MiniTitle>
                                            Custom Fields
                                            <button 
                                                onClick={() => {
                                                    setCustomFields(bef => [...bef, {
                                                        name: "",
                                                        value: "",
                                                    }])
                                                }}
                                                className="px-2 rounded-lg shrink-0 bg-blue-100 text-blue-600 hover:bg-blue-200 ripple cursor-pointer transition">
                                                <RiAddLine className="w-4"/>
                                            </button>
                                        </MiniTitle>
                                        {customFields.length === 0 ? <>
                                            <p className="text-slate-400 font-poppins">No fields added yet.</p>
                                        </>:<>
                                            <div className="space-y-3">
                                                {customFields.map((f, ind) => {
                                                    return (
                                                        <div className="space-y-2" key={ind}>
                                                            <div className="flex gap-2">
                                                                <MiniInput
                                                                    value={f.name}
                                                                    placeholder="Field Name"
                                                                    onChange={(e) => {
                                                                        setCustomFields(bef => bef.map((m, i) => i === ind ? ({
                                                                                ...m,
                                                                                name: e.target.value,
                                                                            }):m
                                                                        ))
                                                                    }}
                                                                />
                                                                <button 
                                                                onClick={() => {
                                                                    setCustomFields(bef => bef.filter((_, i) => i !== ind))
                                                                }}
                                                                className="shrink-0 px-2 cursor-pointer ripple transition bg-red-100 hover:bg-red-200 rounded-lg text-red-600">
                                                                    <RiCloseLine className="w-4"/>
                                                                </button>
                                                            </div>
                                                            <MiniTextArea
                                                            value={f.value}
                                                            onChange={(e) => {
                                                                setCustomFields(bef => bef.map((m, i) => i === ind ? ({
                                                                        ...m,
                                                                        value: e.target.value,
                                                                    }):m
                                                                ))
                                                            }}
                                                            placeholder="Field Value"/>
                                                        </div>
                                                    )
                                                })}
                                            </div>
                                        </>}
                                    </div>
                                    <div>
                                        <MiniTitle>Campaigns</MiniTitle>
                                        <CampaignSelector
                                            onAdd={(id, name) => {
                                                setBody(bef => bef ? ({...bef, campaigns: [...bef.campaigns, {
                                                    id,
                                                    name: name ?? "",
                                                }]}):null)
                                            }}      
                                            onRemove={(id) => {
                                                setBody(bef => bef ? ({...bef, campaigns: bef.campaigns.filter(v => v.id !== id)}):null)
                                            }}
                                            selected={body.campaigns}
                                        />
                                    </div>
                                    <div>
                                        <MiniTitle>Additional Fields</MiniTitle>
                                        <div className="space-y-3">
                                            <BoolField
                                             name="subscribed"
                                             value={body.subscribed}
                                             onChange={() => {
                                                setBody(bef => bef ? ({
                                                    ...bef, subscribed: !bef.subscribed,
                                                }):null);
                                             }}>
                                                <BoolFieldTitle>Subscribed</BoolFieldTitle>
                                            </BoolField>
                                        </div>
                                    </div>
                                    <div className="flex justify-end">
                                        <div className="flex gap-2 relative">
                                            <button
                                             className="bg-slate-200 hover:bg-slate-300 px-3 h-10 text-slate-500 cursor-pointer transition flex items-center justify-center rounded-lg"
                                             onClick={ResetBody}>
                                                Clear
                                            </button>
                                            <button
                                             className={`flex h-10 w-32 ${loading ? "bg-blue-600":"bg-blue-500 hover:bg-blue-600"} ripple text-white flex items-center justify-center rounded-lg cursor-pointer transition`}
                                             onClick={SaveChanges}>
                                                {loading ? <Loading className="h-5"/>:"Save Changes"}
                                            </button>
                                            {(!isChangedCampaigns && !isChangedCustomFields && preview.first_name === body.first_name && preview.last_name === body.last_name && preview.company === body.company && preview.phone === body.phone && preview.email === body.email && preview.subscribed === body.subscribed) && 
                                            <div className="absolute top-0 left-0 w-full h-full bg-white opacity-40 cursor-not-allowed"/>}
                                        </div>
                                    </div>
                                    <p className="text-right text-red-500">{error}</p>
                                </div>
                            </>}
                        </div>
                    </div>
                )
            })()}
            <AddContacts/>
        </ContactsContext.Provider>
    )
}

function BoolField({
    children,
    name,
    value,
    onChange,
}:{
    name: string,
    value: boolean,
    onChange: () => void,
    children: React.ReactNode,
}){
    return (<div>
        <label htmlFor={`edit-contact-${name}`} className="cursor-pointer select-none flex items-center gap-3 jusitfy-center">
            <input 
             type="checkbox" 
             checked={value}
             onChange={onChange}
             className="hidden"
             id={`edit-contact-${name}`}/>
            <Checkbox
                checked={value}/>
            {children}
        </label>
    </div>)
}

function BoolFieldTitle({
    children,
}:{
    children: React.ReactNode,
}){
    return (<h1 className="font-poppins text-slate-500">
        {children}
    </h1>)
}

function SortBySelection({
    search,
    setSearch
}:{
    search: SearchContacts,
    setSearch: React.Dispatch<React.SetStateAction<SearchContacts>>
}){
    const [show, setShow] = React.useState<boolean>(false)
    const ref = React.useRef<HTMLDivElement>(null);

    useClickOutside(ref, () => setShow(false))

    return (<div className="space-y-4">
        <div className="relative" ref={ref}>
          <Selector caret show={show} setShow={setShow}>
            {SortByNames[search.sort_by]}
          </Selector>

          <SelectMenu show={show}>
            {Object.entries(SortByNames).map(([key, label]) => (
              <SelectOption
                key={key}
                selected={search.sort_by === key}
                onClick={() => {
                    setSearch(bef => ({
                        ...bef,
                        sort_by: key as SortBy
                    }))
                    setShow(false);
                }}
              >
                {label}
              </SelectOption>
            ))}
          </SelectMenu>
        </div>
        <div className="flex justify-end px-2">
        <CheckLine
            value={search.reverse}
            setValue={(v) => setSearch(bef => ({
                ...bef,
                reverse: v,
            }))}>
            Reverse
        </CheckLine></div>
    </div>)
}

function CustomFieldRow({
  field,
  onChange,
  onDelete,
}: {
  field: CustomFieldFilter;
  onChange: (f: CustomFieldFilter) => void;
  onDelete: () => void;
}) {
    const [show, setShow] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);

    useClickOutside(ref, () => setShow(false));

    return (
    <div className="space-y-2">
      <div className="grid grid-cols-2 gap-2">
        <MiniInput
          value={field.name}
          placeholder="my_custom_field"
          onChange={(e) => onChange({ ...field, name: e.target.value })}
        />

        <div ref={ref} className="relative">
          <Selector caret show={show} setShow={setShow}>
            {CustomFieldFilterTypeNames[field.type]}
          </Selector>

          <SelectMenu show={show}>
            {Object.entries(CustomFieldFilterTypeNames).map(([key, label]) => (
              <SelectOption
                key={key}
                selected={field.type === key}
                onClick={() => {
                  onChange({ ...field, type: key as CustomFieldFilterType });
                  setShow(false);
                }}
              >
                {label}
              </SelectOption>
            ))}
          </SelectMenu>
        </div>
      </div>

      <div className="flex gap-2">
        <div className="grow">
            <MiniInput
            value={field.value}
            placeholder="my text"
            onChange={(e) => onChange({ ...field, value: e.target.value })}
            />
        </div>
        <button
          type="button"
          className="ripple shrink-0 px-3 cursor-pointer text-red-600 transition bg-red-100 hover:bg-red-200 rounded-lg"
          onClick={onDelete}
        >
          <RiCloseCircleLine className="w-5 h-5" />
        </button>
      </div>
    </div>
    );
}

export function useContacts() {
  return useContext(ContactsContext);
}

export default ContactsProvider;