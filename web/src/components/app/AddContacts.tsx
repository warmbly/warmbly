import { RiAccountCircleLine, RiAccountPinCircleLine, RiAddLine, RiBuildingLine, RiCloseLine, RiCursorLine, RiFileCloudLine, RiGroup2Line, RiInstanceLine, RiLayoutMasonryLine, RiLinkedinBoxFill, RiMapPinLine, RiPagesLine, RiPhoneLine, RiUploadCloud2Line } from "@remixicon/react";
import React from "react";
import { useContacts } from "@/hooks/ContactsProvider";
import MiniInput from "./popup/MiniInput";
import CampaignSelector from "./popup/select/CampaignSelector";
import MiniTextArea from "./popup/MiniTextArea";
import { Loading } from "../loader";
import { APIError, Call } from "@/lib/api";
import Papa, { type ParseResult } from 'papaparse';
import Selector from "./popup/select/Selector";
import useClickOutside from "@/hooks/useClickOutside";
import SelectMenu from "./popup/select/SelectMenu";
import SelectOption from "./popup/select/SelectOption";
import { convertToValidJSONKey, isValidEmail } from "@/lib/helper";
import AddBoxTopBack from "./emails/add/AddBoxTopBack";
import useAddContacts from "@/lib/api/hooks/app/contacts/useAddContacts";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

function MiniTitle({ children, nom }: { children: React.ReactNode, nom?: boolean }) {
    return <h1 className={`text-slate-400 flex items-center gap-2 font-semibold font-inter ${nom ? "mb-0" : "mb-3"} text-sm uppercase tracking-wider`}>{children}</h1>
}

type UploadMethod = 'csv' | 'manual'

export interface AddContact {
    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;
    campaigns: string[];
    custom_fields: Record<string, string>;
}

interface SelectCampaigns {
    id: string;
    name: string;
}

interface ManualContactCustomField {
    name: string;
    value: string;
}

interface ManualContact {
    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;
    custom_fields: ManualContactCustomField[];
    campaigns: SelectCampaigns[];
}

function UploadOption({
    icon,
    children,
    onClick,
}: {
    icon: React.ReactNode,
    children: React.ReactNode,
    onClick?: () => void | Promise<void>,
}) {
    return (<button
        className="flex flex-col w-full md:w-80 ripple gap-5 items-center p-7 rounded-lg border border-gray-300 bg-slate-50 hover:bg-slate-100 transition cursor-pointer"
        onClick={onClick}
    >
        {icon}
        {children}
    </button>)
}
function UploadOptionTitle({
    children,
}: {
    children: React.ReactNode
}) {
    return <h1 className="text-slate-600 mb-2 text-center font-semibold">{children}</h1>
}
function UploadOptionDescription({
    children,
}: {
    children: React.ReactNode
}) {
    return <p className="text-slate-600 mt-2 text-center">{children}</p>
}


function Popup({ value, children }: { value: boolean, children: React.ReactNode }) {
    return <div className={`bg-white duration-200 absolute top-0 right-0 shadow-md p-7 w-[calc(99%-0px)] h-full ml-auto rounded-l-4xl overflow-y-scroll no-scrollbar transition ${value ? "opacity-100 visible translate-x-0" : "opacity-0 invisible translate-x-[20%]"}`}>
        {children}
    </div>
}

const DEFAULT_CONTACT: ManualContact = {
    first_name: "",
    last_name: "",
    email: "",
    company: "",
    phone: "",
    custom_fields: [],
    campaigns: [],
}

const main_fields = ["first_name", "last_name", "email", "company", "phone"]

type ColumnMapping = Record<string, string>;

interface CustomField {
    name: string;
    value: string;
}

export default function AddContacts() {
    const [uploadMethod, setUploadMethod] = React.useState<UploadMethod | null>(null);
    const [error, setError] = React.useState<string>("");
    const [manual, setManual] = React.useState<ManualContact>(DEFAULT_CONTACT)
    const [loading, setLoading] = React.useState<boolean>(false);

    // .csv
    const [rows, setRows] = React.useState<Record<string, string>[]>([]);
    const [csvalues, setCSValues] = React.useState<ColumnMapping>({});
    const [customFields, setCustomFields] = React.useState<CustomField[]>([]);
    const [loadingP, setLoadingP] = React.useState<boolean>(false);
    const [campaigns, setCampaigns] = React.useState<SelectCampaigns[]>([]);
    const [preview, setPreview] = React.useState<AddContact[] | null>(null);

    const c = useContacts();
    const addContacts = useAddContacts();

    React.useEffect(() => {
        setPreview(null);
    }, [c?.add])

    if (!c) return;

    async function AddManual() {
        if (loading) return;
        try {
            setLoading(true);
            const data = {
                first_name: manual.first_name,
                last_name: manual.last_name,
                email: manual.email,
                company: manual.company,
                phone: manual.phone,
                custom_fields: manual.custom_fields.reduce(
                    (acc, { name, value }) => {
                        acc[name] = value;
                        return acc;
                    },
                    {} as Record<string, string>
                ),
                campaigns: manual.campaigns.map((c) => c.id)
            }
            await toast.promise(
                addContacts.mutateAsync([data]),
                {
                    loading: "Loading...",
                    success: "Contact successfully added.",
                    error: (err: AppError) => buildError(err),
                }
            )
        } finally {
            setLoading(false);
        }
    }

    function UploadFile(e: React.ChangeEvent<HTMLInputElement>) {
        const file = e.target.files?.[0];
        if (!file) return;


        Papa.parse(file, {
            header: true,
            skipEmptyLines: true,
            complete: (result: ParseResult<Record<string, string>>) => {
                setRows(result.data);
            },
        });
    }

    const getColumnForField = (
        field: string,
        row: Record<string, string>,
    ): string | null => {
        const entry = Object.entries(csvalues).find(([, value]) => value === field);
        if (entry) {
            return row[entry[0]]
        }

        const fentry = customFields.find((f) => f.name === field);
        if (fentry) {
            return interpolate(fentry.value, row)
        }

        return null;
    };

    const interpolate = (template: string, row: Record<string, string>): string =>
        template.replace(/\{\{(\w+)\}\}/g, (_, col) => row[col.toLowerCase()] || '');

    function LoadCSVContacts(): AddContact[] {
        const entries: AddContact[] = rows.map((row, ind) => {
            const custom_fields: Record<string, string> = {};
            const primary: Record<string, string> = {}

            main_fields.map((v) => {
                primary[v] = getColumnForField(v, row) ?? "";
            })

            Object.entries(row).map(([name, v]) => {
                const cv = csvalues[name] ?? "";
                if (!main_fields.find((x) => x === cv)) {
                    if (v === "custom") {
                        custom_fields[convertToValidJSONKey(name)] = v
                    } else if (cv) {
                        custom_fields[cv] = v
                    }
                }
            })

            customFields.map((f) => {
                if (!main_fields.find((x) => x === f.name)) {
                    custom_fields[convertToValidJSONKey(f.name)] = f.value;
                }
            })

            const email = primary['email'] ?? ""
            if (!isValidEmail(email)) {
                throw new Error(`Invalid email address at row ${ind} ("${email}")`)
            }

            return {
                first_name: primary['first_name'] ?? "",
                last_name: primary['last_name'] ?? "",
                email: email,
                company: primary['company'] ?? "",
                phone: primary['phone'] ?? "",
                campaigns: (c?.add && c?.add.campaigns.length > 0) ? c.add.campaigns : campaigns.map((c) => c.id),
                custom_fields,
            }
        })
        return entries
    }

    function Preview() {
        try {
            setLoadingP(true);
            const prev = LoadCSVContacts();
            setPreview(prev)
        } catch (err) {
            setError("Client Error: " + String(err))
        } finally {
            setLoadingP(false);
        }
    }

    async function AddCSV() {
        try {
            setLoading(true);
            const prev = LoadCSVContacts();
            await Call("/contacts", "POST", prev)
        } catch (err) {
            if (err instanceof APIError) {
                setError(`${err.message}: ${err.body.message}`)
            } else {
                setError(`Client Error: ${err}`)
            }
        } finally {
            setLoading(false);
        }
    }


    return (<>
        <div className={`fixed inset-0 z-100 bg-slate-950/45 flex justify-center items-center transition ${c.add ? "opacity-100 visible" : "opacity-0 invisible"}`}>
            <Popup value={c.add !== null}>
                <div className="absolute top-10 right-10 z-101 text-slate-500 hover:text-slate-400 transition cursor-pointer"
                    onClick={() => {
                        c.setAdd(null);
                        setError("");
                        setUploadMethod(null);
                    }}
                >
                    <RiCloseLine className="w-5" />
                </div>
                <h1 className="text-5xl text-slate-600 font-bold font-inter mb-9 mr-4 text-center mt-12">Upload Contacts</h1>
                <p className="text-xl text-slate-400 font-inter max-w-5xl mx-auto text-center mb-14">Easily grow your contact list by adding new people in two ways: manually enter individual details, or quickly import multiple contacts at once using a .csv file. Choose the option that best fits your workflow to start organizing and reaching your audience faster.</p>
                <div className="flex flex-col md:flex-row justify-center gap-8 mb-8">
                    <UploadOption
                        onClick={() => setUploadMethod('manual')}
                        icon={<RiCursorLine className="w-8 shrink-0 text-slate-600" />}>
                        <div>
                            <UploadOptionTitle>Enter Manually</UploadOptionTitle>
                            <UploadOptionDescription>Enter Contact Details Manually</UploadOptionDescription>
                        </div>
                    </UploadOption>
                    <div className="flex md:flex-col items-center gap-5 md:gap-3 text-slate-300">
                        <div className="grow h-px md:h-0 md:w-px bg-slate-300" />
                        <span>or</span>
                        <div className="grow h-px md:h-0 md:w-px bg-slate-300" />
                    </div>
                    <UploadOption
                        onClick={() => setUploadMethod('csv')}
                        icon={<RiUploadCloud2Line className="w-8 shrink-0 text-green-500" />}>
                        <div>
                            <UploadOptionTitle>Upload .csv</UploadOptionTitle>
                            <UploadOptionDescription>Bulk Upload Contacts</UploadOptionDescription>
                        </div>
                    </UploadOption>
                </div>
            </Popup>
            <Popup value={uploadMethod === 'manual'}>
                <AddBoxTopBack onClick={() => setUploadMethod(null)}>Enter Manually</AddBoxTopBack>
                <div className="flex justify-center">
                    <div className="max-w-3xl w-full space-y-4">
                        <div className="flex gap-5 items-center">
                            <RiCursorLine className="h-7 w-8 shrink-0" />
                            <div>
                                <h1 className="text-lg">Manual Contact</h1>
                                <p className="text-slate-400">Please fill out the following fields</p>
                            </div>
                        </div>
                        <div className="grid grid-cols-2 gap-3">
                            <div>
                                <MiniTitle>First Name</MiniTitle>
                                <MiniInput
                                    value={manual.first_name}
                                    placeholder="e.g. John"
                                    onChange={(e) => setManual(bef => ({ ...bef, first_name: e.target.value }))} />
                            </div>
                            <div>
                                <MiniTitle>Last Name</MiniTitle>
                                <MiniInput
                                    value={manual.last_name}
                                    placeholder="e.g. Doe"
                                    onChange={(e) => setManual(bef => ({ ...bef, last_name: e.target.value }))} />
                            </div>
                        </div>
                        <div>
                            <MiniTitle>Email</MiniTitle>
                            <MiniInput
                                value={manual.email}
                                placeholder="e.g. name@example.com"
                                onChange={(e) => setManual(bef => ({ ...bef, email: e.target.value }))} />
                        </div>
                        <div>
                            <MiniTitle>Company</MiniTitle>
                            <MiniInput
                                value={manual.company}
                                placeholder="e.g. Acme Inc."
                                onChange={(e) => setManual(bef => ({ ...bef, company: e.target.value }))} />
                        </div>
                        <div>
                            <MiniTitle>Phone</MiniTitle>
                            <MiniInput
                                value={manual.phone}
                                placeholder="e.g. +1 (123) 456-7890"
                                onChange={(e) => setManual(bef => ({ ...bef, phone: e.target.value }))} />
                        </div>
                        <div>
                            <div className="flex items-center gap-2 mb-3">
                                <MiniTitle nom>Custom Fields</MiniTitle>
                                <button
                                    onClick={() => {
                                        setManual(bef => ({
                                            ...bef,
                                            custom_fields: [...bef.custom_fields, { name: "", value: "" }]
                                        }))
                                    }}
                                    className="px-2 rounded-lg shrink-0 bg-blue-100 text-blue-600 hover:bg-blue-200 ripple cursor-pointer transition">
                                    <RiAddLine className="w-4" />
                                </button>
                            </div>
                            {manual.custom_fields.length === 0 ? <>
                                <p className="text-slate-400 font-poppins">No fields added yet.</p>
                            </> : <>
                                <div className="space-y-3">
                                    {manual.custom_fields.map((f, ind) => {
                                        return (
                                            <div className="space-y-2" key={ind}>
                                                <div className="flex gap-2">
                                                    <MiniInput
                                                        value={f.name}
                                                        placeholder="Field Name"
                                                        onChange={(e) => {
                                                            setManual(bef => ({
                                                                ...bef,
                                                                custom_fields: bef.custom_fields.map((m, i) => i === ind ? ({
                                                                    ...m,
                                                                    name: e.target.value,
                                                                }) : m)
                                                            }))
                                                        }}
                                                    />
                                                    <button
                                                        onClick={() => {
                                                            setManual(bef => ({
                                                                ...bef,
                                                                custom_fields: bef.custom_fields.filter((_, i) => i !== ind)
                                                            }))
                                                        }}
                                                        className="shrink-0 px-2 cursor-pointer ripple transition bg-red-100 hover:bg-red-200 rounded-lg text-red-600">
                                                        <RiCloseLine className="w-4" />
                                                    </button>
                                                </div>
                                                <MiniTextArea
                                                    value={f.value}
                                                    onChange={(e) => {
                                                        setManual(bef => ({
                                                            ...bef,
                                                            custom_fields: bef.custom_fields.map((m, i) => i === ind ? ({
                                                                ...m,
                                                                value: e.target.value,
                                                            }) : m)
                                                        }))
                                                    }}
                                                    placeholder="Field Value" />
                                            </div>
                                        )
                                    })}
                                </div>
                            </>}
                        </div>
                        {c.add?.campaigns.length === 0 && (
                            <div>
                                <MiniTitle>Campaigns</MiniTitle>
                                <CampaignSelector
                                    selected={manual.campaigns}
                                    onAdd={(id, name) => {
                                        setManual(bef => ({
                                            ...bef,
                                            campaigns: [...bef.campaigns, {
                                                id: id,
                                                name: name ?? "",
                                            }]
                                        }))
                                    }}
                                    onRemove={(id) => {
                                        setManual(bef => ({
                                            ...bef,
                                            campaigns: bef.campaigns.filter((c) => c.id !== id)
                                        }))
                                    }}
                                    reverse
                                />
                            </div>)}
                        <div className="flex justify-end gap-3">
                            <button
                                onClick={() => setManual(DEFAULT_CONTACT)}
                                className="ripple rounded-lg bg-slate-200 cursor-pointer hover:bg-slate-300 transition text-slate-500 h-10 w-20">
                                Reset
                            </button>
                            <button
                                className={`ripple rounded-lg cursor-pointer ${loading ? "bg-blue-600" : "bg-blue-500 hover:bg-blue-600"} transition text-white h-10 w-30 flex items-center justify-center`}
                                onClick={AddManual}
                            >
                                {loading ? <Loading className="h-4" /> : "Add Contact"}
                            </button>
                        </div>
                    </div>
                </div>
            </Popup>
            <Popup value={uploadMethod === 'csv'}>
                <AddBoxTopBack onClick={() => setUploadMethod(null)}>Bulk Import</AddBoxTopBack>
                <div className="flex justify-center">
                    <div className="max-w-3xl w-full space-y-4">
                        <div className="flex gap-5 justify-between items-center">
                            <div className="flex gap-5 items-center">
                                <RiUploadCloud2Line className="h-7 w-8 shrink-0" />
                                <div>
                                    <h1 className="text-lg">Import from .csv</h1>
                                    <p className="text-slate-400">Please fill out the following fields</p>
                                </div>
                            </div>
                            <div>
                                <button
                                    disabled={rows.length === 0}
                                    onClick={() => setRows([])}
                                    className={`bg-red-100 text-red-500 px-4 rounded-lg py-2 ${rows.length > 0 ? "cursor-pointer hover:bg-red-200" : "cursor-not-allowed opacity-50"}`}
                                >Reset</button>
                            </div>
                        </div>
                        <div className="flex justify-start gap-5 items-center">
                            <label
                                htmlFor="contacts-csv-upload"
                                className={`
                                flex items-center gap-2
                                px-5 py-2.5 border border-gray-300
                                bg-white hover:bg-gray-100
                                font-medium text-sm rounded-lg shadow-md
                                transition-all duration-200 cursor-pointer
                                focus-within:outline-none focus-within:ring-2 focus-within:ring-offset-2 focus-within:ring-gray-300
                                `}
                            >
                                <RiFileCloudLine size={17} />
                                Upload .csv file
                                <input
                                    id="contacts-csv-upload"
                                    type="file"
                                    accept=".csv"
                                    className="sr-only hidden"
                                    onChange={UploadFile}
                                />
                            </label>
                            {rows.length > 0 && <span className="text-slate-500 font-poppins">{rows.length} rows selected</span>}
                        </div>
                        {rows.length > 0 && <>
                            <div className="space-y-3">
                                {Object.entries(rows[0]).map(([name], index) => {
                                    return (
                                        <CSVSelector
                                            key={index}
                                            column={name}
                                            selection={csvalues[name] || ""}
                                            onChange={(v) => {
                                                setCSValues(bef => ({
                                                    ...bef,
                                                    [name]: v,
                                                }))
                                            }}
                                        />
                                    )
                                })}
                            </div>
                            <div>
                                <div className="flex items-center gap-2 mb-3">
                                    <MiniTitle nom>Extra Fields</MiniTitle>
                                    <button
                                        onClick={() => {
                                            setCustomFields(bef => [...bef, {
                                                name: "",
                                                value: "",
                                            }])
                                        }}
                                        className="px-2 rounded-lg shrink-0 bg-blue-100 text-blue-600 hover:bg-blue-200 ripple cursor-pointer transition">
                                        <RiAddLine className="w-4" />
                                    </button>
                                </div>
                                {customFields.length === 0 ? <>
                                    <p className="text-slate-400 font-poppins">No fields added yet.</p>
                                </> : <>
                                    <div className="space-y-3">
                                        {customFields.map((f, ind) => {
                                            return (
                                                <div className="space-y-2" key={ind}>
                                                    <div className="flex gap-2">
                                                        <MiniInput
                                                            value={f.name}
                                                            placeholder="key"
                                                            onChange={(e) => {
                                                                setCustomFields(bef => bef.map((m, i) => i === ind ? ({
                                                                    ...m,
                                                                    name: e.target.value,
                                                                }) : m)
                                                                )
                                                            }}
                                                        />
                                                        <button
                                                            onClick={() => {
                                                                setCustomFields(bef => bef.filter((_, i) => i !== ind))
                                                            }}
                                                            className="shrink-0 px-2 cursor-pointer ripple transition bg-red-100 hover:bg-red-200 rounded-lg text-red-600">
                                                            <RiCloseLine className="w-4" />
                                                        </button>
                                                    </div>
                                                    <MiniTextArea
                                                        value={f.value}
                                                        onChange={(e) => {
                                                            setCustomFields(bef => bef.map((m, i) => i === ind ? ({
                                                                ...m,
                                                                value: e.target.value,
                                                            }) : m)
                                                            )
                                                        }}
                                                        placeholder="{{my_column}}" />
                                                </div>
                                            )
                                        })}
                                    </div>
                                </>}
                            </div>
                            {c.add?.campaigns.length === 0 && (
                                <div>
                                    <MiniTitle>Campaigns</MiniTitle>
                                    <CampaignSelector
                                        selected={campaigns}
                                        onAdd={(id, name) => {
                                            setCampaigns(bef => [...bef, {
                                                id: id,
                                                name: name ?? "",
                                            }])
                                        }}
                                        onRemove={(id) => {
                                            setCampaigns(bef => bef.filter((c) => c.id !== id))
                                        }}
                                        reverse
                                    />
                                </div>)}
                            <div className="flex justify-end gap-3">
                                <button
                                    onClick={() => setCSValues({})}
                                    className="ripple rounded-lg bg-slate-200 cursor-pointer hover:bg-slate-300 transition text-slate-500 h-10 w-20">
                                    Clear
                                </button>
                                <button
                                    className={`ripple rounded-lg cursor-pointer ${loading ? "bg-blue-200" : "bg-blue-100 hover:bg-blue-200"} transition text-blue-500 h-10 w-31 flex items-center justify-center`}
                                    onClick={Preview}
                                >
                                    {loadingP ? <Loading className="h-4" /> : "Preview"}
                                </button>
                                <button
                                    className={`ripple rounded-lg cursor-pointer ${loading ? "bg-blue-600" : "bg-blue-500 hover:bg-blue-600"} transition text-white h-10 w-31 flex items-center justify-center`}
                                    onClick={AddCSV}
                                >
                                    {loading ? <Loading className="h-4" /> : "Add Contacts"}
                                </button>
                            </div>
                            {error && <p className="text-right text-red-500">Something went wrong</p>}
                        </>}
                        {preview && <>
                            <div>
                                <div className="flex items-center gap-3 mb-3">
                                    <MiniTitle nom>Preview ({preview.length})</MiniTitle>
                                    <button
                                        onClick={() => setPreview(null)}
                                        className="px-2 shrink-0 rounded-lg bg-red-100 text-red-600 hover:bg-red-200 ripple cursor-pointer transition">
                                        <RiCloseLine className="w-4" />
                                    </button>
                                </div>
                                <div className="grid gap-10 sm:grid-cols-2 md:grid-cols-3">
                                    {preview.map((preview, ind) => {
                                        return (
                                            <div key={`csv-preview-${ind}`} className="space-y-3 border-b pb-11 relative pt-2 border-slate-200">
                                                <span className="absolute top-2 right-0 text-slate-400">#{ind + 1}</span>
                                                <div>
                                                    <MiniTitle>first_name</MiniTitle>
                                                    <CSValue>{preview.first_name}</CSValue>
                                                </div>
                                                <div>
                                                    <MiniTitle>last_name</MiniTitle>
                                                    <CSValue>{preview.first_name}</CSValue>
                                                </div>
                                                <div>
                                                    <MiniTitle>email</MiniTitle>
                                                    <CSValue>{preview.email}</CSValue>
                                                </div>
                                                <div>
                                                    <MiniTitle>company</MiniTitle>
                                                    <CSValue>{preview.company}</CSValue>
                                                </div>
                                                <div>
                                                    <MiniTitle>phone</MiniTitle>
                                                    <CSValue>{preview.company}</CSValue>
                                                </div>
                                                <div>
                                                    <MiniTitle>Custom Fields</MiniTitle>
                                                    <CSValue>
                                                        {Object.keys(preview.custom_fields).length > 0 ?
                                                            <div className="space-y-2">
                                                                {Object.entries(preview.custom_fields).map(([n, v]) => {
                                                                    return (<div key={n}>
                                                                        <b>{n}</b>: {v}
                                                                    </div>)
                                                                })}
                                                            </div> : <span>No custom fields</span>}
                                                    </CSValue>
                                                </div>
                                            </div>
                                        )
                                    })}
                                </div>
                            </div>
                        </>}
                    </div>
                </div>
            </Popup>
        </div>
    </>)
}

function CSValue({
    children
}: {
    children: React.ReactNode
}) {
    return <p className="text-slate-600">
        {children}
    </p>
}

const selectOptions = [
    {
        icon: <RiAccountCircleLine className="w-full text-green-500" />,
        title: "First Name",
        value: "first_name",
    },
    {
        icon: <RiGroup2Line className="w-full text-green-600" />,
        title: "Last Name",
        value: "last_name",
    },
    {
        icon: <RiBuildingLine className="w-full text-blue-500" />,
        title: "Company",
        value: "company",
    },
    {
        icon: <RiPhoneLine className="w-full text-orange-400" />,
        title: "Phone",
        value: "phone",
    },
    {
        icon: <RiMapPinLine className="w-full text-green-500" />,
        title: "Location",
        value: "location",
    },
    {
        icon: <RiInstanceLine className="w-full text-amber-500" />,
        title: "Industry",
        value: "industry",
    },
    {
        icon: <RiAccountPinCircleLine className="w-full text-blue-500" />,
        title: "Employees",
        value: "employees",
    },
    {
        icon: <RiPagesLine className="w-full text-slate-500" />,
        title: "Website",
        value: "website",
    },
    {
        icon: <RiLinkedinBoxFill className="w-full text-blue-600" />,
        title: "LinkedIn",
        value: "linkedin",
    },
    {
        icon: <RiLayoutMasonryLine className="w-full text-blue-600" />,
        title: "Custom Variable",
        value: "custom",
    }
]

function CSVSelector({
    column,
    selection,
    onChange,
}: {
    column: string,
    selection: string,
    onChange: (v: string) => void
}) {
    const [drop, setDrop] = React.useState<boolean>(false);
    const dropRef = React.useRef<HTMLDivElement>(null);

    useClickOutside(dropRef, () => setDrop(false))

    return (<>
        <div className="flex items-center gap-5 justify-between">
            <h1 className="break-all">{column}</h1>
            <div className="relative max-w-sm w-full shrink-0" ref={dropRef}>
                <Selector show={drop} setShow={(v) => setDrop(v)} caret>
                    {(() => {
                        const item = selectOptions.find((o) => o.value === selection);
                        if (item) {
                            return <div className="flex gap-3 items-center">
                                <div className="shrink-0 w-5">
                                    {item.icon}
                                </div>
                                <p>{item.title}</p>
                            </div>
                        }
                        return <>
                            <div>
                                <p className="text-slate-400">Unset</p>
                            </div>
                        </>
                    })()}
                </Selector>
                <SelectMenu show={drop}>
                    {selectOptions.map((o) => {
                        const isSelected = selection === o.value;

                        return (
                            <SelectOption
                                key={o.value}
                                onClick={() => {
                                    if (isSelected) {
                                        onChange("");
                                    } else {
                                        onChange(o.value)
                                    }
                                }}
                                selected={isSelected}>
                                <div className="w-4 shrink-0">
                                    {o.icon}
                                </div>
                                <span className="truncate">{o.title}</span>
                            </SelectOption>
                        )
                    })}
                </SelectMenu>
            </div>
        </div>
    </>)
}
