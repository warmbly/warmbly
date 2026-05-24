"use client";

import MiniInput from '@/components/app/popup/MiniInput';
import CampaignSelector from '@/components/app/popup/select/CampaignSelector';
import Selector from '@/components/app/popup/select/Selector';
import { RiAddLine, RiCloseLine } from '@remixicon/react';
import React, { createContext, useContext } from 'react';
import useClickOutside from './useClickOutside';
import SelectMenu from '@/components/app/popup/select/SelectMenu';
import SelectOption from '@/components/app/popup/select/SelectOption';
import MiniTextArea from '@/components/app/popup/MiniTextArea';
import { CheckFilter, useContacts } from './ContactsProvider';
import Switch from '@/components/app/Switch';
import { APIError, Call } from '@/lib/api';
import { Loading } from '@/components/loader';

interface BulkEditContactsContextType {
    setSelected: React.Dispatch<React.SetStateAction<string[] | null>>,
}

export const BulkEditContactsContext = createContext<BulkEditContactsContextType | null>(null);

interface Campaign {
    id: string;
    name: string;
}

type FieldType = 'ADD' | 'EDIT' | 'DELETE' | 'RENAME'

const FieldTypes: FieldType[] = ["ADD", "EDIT", "DELETE", "RENAME"]

interface Field {
    type: FieldType;
    key: string;
    value: string;
}

function MiniTitle({children}:{children: React.ReactNode}){
    return <h1 className="text-slate-500 font-semibold font-sans mb-3 uppercase tracking-wider text-sm flex items-center gap-2">{children}</h1>
}

export const BulkEditContactsProvider = ({ children }: { children: React.ReactNode }) => {
    const [selected, setSelected] = React.useState<string[] | null>(null);
    const [campaignsAdd, setCampaignsAdd] = React.useState<Campaign[]>([]);
    const [campaignsRemove, setCampaignsRemove] = React.useState<Campaign[]>([]);
    const [fields, setFields] = React.useState<Field[]>([]);
    const [subscribe, setSubscribe] = React.useState<boolean | null>(null);
    const [error, setError] = React.useState<string>("");
    const [loading, setLoading] = React.useState<boolean>(false);

    const c = useContacts();

    const [mouseDownOnButton, setMouseDownOnButton] = React.useState(false);
    const handleMouseDown = () => setMouseDownOnButton(true);
    const handleMouseUp = () => {
        if (mouseDownOnButton) {
            setSelected(null)
        }
        setMouseDownOnButton(false);
    }

    async function Submit() {
        try {
            setLoading(true)
            const cselected = selected;
            const ccampaignsAdd = campaignsAdd;
            const ccampaignsRemove = campaignsRemove;
            const csubscribe = subscribe;
            const cfields = fields;
            await Call("/contacts", "PATCH", {
                contacts: cselected,
                add_campaigns: ccampaignsAdd.map((c) => c.id),
                remove_campaigns: ccampaignsRemove.map((c) => c.id),
                fields: cfields,
                subscribe: csubscribe,
            })
            c?.setContacts(bef => bef ? bef.map((con) => {
                if (cselected?.some(s => s === con.id)){
                    const def_campaigns = con.campaigns.filter(camp => !ccampaignsRemove.some((v) => v.id === camp.id))
                    const custom_fields = con.custom_fields;

                    cfields.forEach((f) => {
                        switch (f.type) {
                            case "DELETE":
                                delete custom_fields[f.key]
                                break;
                            case "RENAME":
                                custom_fields[f.value] = custom_fields[f.key]
                                delete custom_fields[f.key]
                                break;
                            default:
                                custom_fields[f.key] = f.value
                                break;
                        }
                    })

                    return {
                        ...con,
                        campaigns: [...def_campaigns, ...ccampaignsAdd.filter(c => !def_campaigns.some((v) => v.id == c.id))],
                        subscribed: csubscribe !== null ? csubscribe:con.subscribed,
                        custom_fields,
                    }
                } else return con
            }):null)  
            setSelected(null);
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
        <BulkEditContactsContext.Provider value={{setSelected}}>
            {children}
            <div onMouseDown={handleMouseDown} onMouseUp={handleMouseUp} className={`fixed inset-0 z-[900] flex justify-end bg-slate-950/45 transition ${selected ? "visible opacity-100":"invisible opacity-0"}`}>
                <div onMouseDown={(e) => e.stopPropagation()} onMouseUp={(e) => e.stopPropagation()} className={`flex p-10 flex-col bg-white relative w-200 max-w-[95%] h-full transition-transform ${selected ? "translate-x-0":"translate-x-100"}`}>
                    <div className='space-y-5'>
                        <div>
                            <h1 className='text-lg text-slate-600 font-poppins mb-8 font-semibold'>Edit {selected ? selected.length:0} selected contact(s)</h1>
                        </div>
                        <div>
                            <MiniTitle>Campaigns to add</MiniTitle>
                            <CampaignSelector
                            onAdd={(id, name) => {
                                setCampaignsAdd(bef => [...bef, {
                                    id,
                                    name: name ?? "",
                                }])
                            }}
                            onRemove={(id) => {
                                setCampaignsAdd(bef => bef.filter(v => v.id !== id))
                            }}
                            selected={campaignsAdd}
                            />
                        </div>
                        <div>
                            <MiniTitle>Campaigns to remove</MiniTitle>
                            <CampaignSelector
                            onAdd={(id, name) => {
                                setCampaignsRemove(bef => [...bef, {
                                    id,
                                    name: name ?? "",
                                }])
                            }}
                            onRemove={(id) => {
                                setCampaignsRemove(bef => bef.filter(v => v.id !== id))
                            }}
                            selected={campaignsRemove}
                            />
                        </div>
                        <div>
                            <MiniTitle>
                                Custom Fields
                                {fields.length < 100 &&
                                <button 
                                 onClick={() => {
                                    setFields(bef => [...bef, {
                                        key: "",
                                        type: "ADD",
                                        value: "",
                                    }])
                                 }}
                                 className='ripple bg-blue-100 hover:bg-blue-200 transition rounded-lg px-1 text-blue-600 cursor-pointer'>
                                    <RiAddLine className='w-4'/>
                                </button>}
                            </MiniTitle>
                            {fields.length > 0 ? 
                            <div className='space-y-1'>
                                {fields.map((f, i) => {
                                    return (
                                        <FieldEdit
                                         key={i}
                                         field={f}
                                         index={i}
                                         setFields={setFields}
                                        />
                                    )
                                })}
                            </div>:<div>
                                <p className='text-slate-400'>
                                    No fields added yet.
                                </p>
                            </div>}
                        </div>
                        <div>
                            <MiniTitle>
                                Fields
                            </MiniTitle>
                            <CheckFilter
                                value={subscribe !== null}
                                setValue={(v) => {
                                    if (v) {
                                        setSubscribe(true)
                                    } else {
                                        setSubscribe(null)
                                    }
                                }}
                                label="Subscribed"
                            >
                                {subscribe !== null && (
                                    <Switch
                                        id="contact-edit-subscribed"
                                        value={subscribe}
                                        onChange={(v) => setSubscribe(v)}
                                    />
                                )}
                            </CheckFilter>
                        </div>
                        <div className='flex justify-end'>
                            <div className='relative flex gap-2 items-center'>
                                <button
                                 className={`bg-slate-200 hover:bg-slate-300 transition w-20 h-10 flex items-center justify-center text-slate-500 rounded-lg cursor-pointer`}
                                 onClick={() => {
                                    setCampaignsAdd([])
                                    setCampaignsRemove([])
                                    setSubscribe(null)
                                    setFields([])
                                 }}>
                                    Clear
                                </button>
                                <button
                                 className={`${loading ? "bg-blue-600":"bg-blue-500 hover:bg-blue-600"} transition w-32 h-10 flex items-center justify-center text-white rounded-lg cursor-pointer`}
                                 onClick={Submit}>
                                    {loading ? <Loading className='h-4'/>:"Make Changes"}
                                </button>
                                {(campaignsAdd.length === 0 && campaignsRemove.length === 0 && fields.length === 0 && subscribe === null) && <div className='bg-white opacity-40 absolute top-0 left-0 w-full h-full cursor-not-allowed'/>}
                            </div>
                        </div>
                        {error && <p className='text-right text-red-500'>{error}</p>}
                    </div>
                </div>
            </div>
        </BulkEditContactsContext.Provider>
    )
};

function FieldEdit({
    field,
    setFields,
    index
}:{
    field: Field,
    setFields: React.Dispatch<React.SetStateAction<Field[]>>,
    index: number,
}){
    const [show, setShow] = React.useState<boolean>(false);
    const dropRef = React.useRef<HTMLDivElement>(null)

    useClickOutside(dropRef, () => setShow(false))
    return (
        <div className='space-y-2'>
            <div className='flex gap-2'>
                <div className='grid grid-cols-2 gap-2 grow'>
                    <MiniInput
                        value={field.key}
                        placeholder='Key'
                        onChange={(e) => {
                        setFields(bef => bef.map((f, i) => i === index ? ({
                            ...f,
                            key: e.target.value,
                        }):f))
                        }}
                    />
                    <div className='relative' ref={dropRef}>
                        <Selector
                        show={show}
                        setShow={setShow}
                        caret
                        >
                            {field.type}
                        </Selector>
                        <SelectMenu
                            show={show}
                            >
                            {FieldTypes.map((v, ind) => {
                                return (
                                    <SelectOption
                                        key={ind}
                                        selected={field.type === v}
                                        onClick={() => {
                                            setFields(bef => bef.map((x, i) => i === index ? ({
                                                ...x,
                                                type: v,
                                            }):x));
                                            setShow(false);
                                        }}
                                    >
                                        {v}
                                    </SelectOption>
                                )
                            })}
                        </SelectMenu>
                    </div>
                </div>
                <button 
                 className='ripple shrink-0 w-10 flex border border-transparent rounded-lg items-center justify-center bg-red-100 hover:bg-red-200 transition cursor-pointer text-red-500'
                 onClick={() => setFields(bef => bef.filter((_, ind) => ind !== index))}>
                    <RiCloseLine className='w-4'/>
                </button>
            </div>
            <MiniTextArea
             value={field.type !== "DELETE" ? field.value:""}
             disabled={field.type === "DELETE"}
             placeholder='Value'
             onChange={(e) => {
                setFields(bef => bef.map((v, i) => i === index ? ({
                    ...v,
                    value: e.target.value
                }):v))
             }}/>
        </div>
    )
}

export default BulkEditContactsProvider;

export function useBulkEditContacts() {
  return useContext(BulkEditContactsContext);
}