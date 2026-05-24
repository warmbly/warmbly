"use client";

import { APIError, Call } from "@/lib/api";
import React, { createContext, useContext } from "react";
import { useError } from "./ErrorProvider";
import { Input, TextArea } from "@/components/input";
import { RiCloseLine } from "@remixicon/react";
import { Loading } from "@/components/loader";

export type CampaignStatus = 'draft' | 'active' | 'paused';

export interface SequenceRaw {
    id: string;
    name: string;

    subject: string;

    body_plain: string;
    body_html: string;
    body_sync: boolean;
    body_code: boolean;

    wait_after: number;

    updated_at: string;
    created_at: string;
}

export interface Sequence extends Omit<SequenceRaw, 'updated_at' | 'created_at'> {
    updatedAt: Date,
    createdAt: Date,
}

export function parseSequence(raw: SequenceRaw): Sequence {
    return {
        ...raw,
        updatedAt: new Date(raw.updated_at),
        createdAt: new Date(raw.created_at),
    }
}

export function parseSequences(raw: SequenceRaw[]): Sequence[] {
    return raw.map((s) => parseSequence(s))
}

export interface CampaignRaw {
    id: string;

    name: string;
    description: string;
    status: CampaignStatus;

    stop_on_reply: boolean;
    open_tracking: boolean;
    link_tracking: boolean;
    text_only: boolean;
    daily_limit: number;
    unsubscribe_header: boolean;
    risky_emails: boolean;

    cc: string[];
    bcc: string[];

    start_date?: string | null;
    end_date?: string | null;
    timezone: string;
    days: number;
    start_time: string;
    end_time: string;

    email_tags: string[];
    
    updated_at: string;
    created_at: string;

    // Extra
    analytics: any | null;
    sequences: Sequence[] | null;
}

export interface Campaign extends Omit<CampaignRaw, 'start_date' | 'end_date' | 'updated_at' | 'created_at'> {
    startDate: Date | null;
    endDate: Date | null;
    updatedAt: Date;
    createdAt: Date;
}

export function parseCampaign(raw: CampaignRaw): Campaign {
    return {
        ...raw,
        startDate: raw.start_date ? new Date(raw.start_date) : null,
        endDate: raw.end_date ? new Date(raw.end_date) : null,
        updatedAt: new Date(raw.updated_at),
        createdAt: new Date(raw.created_at),
    }
}

export function parseCampaigns(raw: CampaignRaw[]): Campaign[] {
    return raw.map((i) => parseCampaign(i))
}

interface CampaignContextType {
  loading: boolean,
  campaigns: Campaign[] | null,
  max: number,
  getCampaigns: (s?: string, f?: string) => Promise<void>,
  newCampaign: (v: boolean) => void,
  GetCampaigns: () => Promise<void>,
  GetCampaignsQ: (search: string) => Promise<void>,
  getCampaign: (id: string) => Promise<Campaign>,
  updateCampaign: (campaign: Campaign) => void,

  fetchSequences: (id: string) => Promise<Sequence[]>,
  updateSequences: (id: string, sequences: Sequence[]) => void,
}

export const CampaignContext = createContext<CampaignContextType | undefined>(undefined);

function MiniTitle({children}:{children: React.ReactNode}){
    return <h1 className="text-slate-500 font-semibold font-sans mb-2 text-lg">{children}</h1>
}

export const CampaignProvider = ({ children }: { children: React.ReactNode }) => {
    const { showError } = useError();

    const [campaigns, setCampaigns] = React.useState<Campaign[] | null>(null);
    const [add, setAdd] = React.useState<boolean>(false);
    const [name, setName] = React.useState<string>("");
    const [description, setDescription] = React.useState<string>("")
    const [newLoad, setNewLoad] = React.useState<boolean>(false);
    const [max, setMax] = React.useState<number>(0);
    const [error, setError] = React.useState<string>("");
    const [search, setSearch] = React.useState<string>("");
    const [loading, setLoading] = React.useState<boolean>(false);
    const [folder, setFolder] = React.useState<string>("");

    const getCampaigns = async (s?: string, f?: string) => {
        if (max && s === undefined) return;
        try {
            if (s !== undefined) setCampaigns(null);
            const params = new URLSearchParams();
            let isNew = false;
            if (campaigns && campaigns.length > 0 && s === undefined && f === undefined){
                params.set("from", campaigns[campaigns.length-1].id)
                isNew = true;
            }
            if (s !== undefined) {
                params.set("q", s)
            } else if (search) {
                params.set("q", search)
            }
            if (f !== undefined) {
                params.set("folder", f)
            } else if (folder) {
                params.set("folder", folder)
            }
            let campaigns_raw: CampaignRaw[];
            if (!isNew) {
                const data = await Call(`/campaigns?${params.toString()}`);
                setMax(data.count)
                campaigns_raw = data.data;
            } else {
                campaigns_raw = await Call(`/campaigns?${params.toString()}`);
            }
            const campaignsNew = parseCampaigns(campaigns_raw)
            setCampaigns(bef => bef ? [...bef, ...campaignsNew].sort((a, b) => a.createdAt.getTime()-b.createdAt.getTime()).filter(
                (c, index, arr) => arr.findIndex(x => x.id === c.id) === index
                ):campaignsNew);
            if (s) setSearch(s);
            if (f) setFolder(f);
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

    const GetCampaigns = async () => {
        await getCampaigns();
    }

    const GetCampaignsQ = async (search: string) => {
        await getCampaigns(search)
    }

    const updateCampaign = (campaign: Campaign) => {
        setCampaigns(bef => bef ? bef.map((c) => c.id === campaign.id ? {...campaign, analytics: c.analytics}:c):null)
    }

    const newCampaign = async () => {
        if (!newLoad){
            try {
                setNewLoad(true)
                const campaign: CampaignRaw = await Call(`/campaigns`, "POST", {
                    name,
                    description,
                })
                setCampaigns(bef => bef ? [parseCampaign(campaign), ...bef]:[parseCampaign(campaign)])
                setAdd(false);
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

    const getCampaign = async (id: string) => {
        const campaign_raw: CampaignRaw = await Call(`/campaigns/${id}`)
        const campaign = parseCampaign(campaign_raw)
        setCampaigns(bef => bef ? [...bef, campaign].sort((a, b) => a.createdAt.getTime()-b.createdAt.getTime()).filter(
            (c, index, arr) => arr.findIndex(x => x.id === c.id) === index
            ):[campaign])
        return campaign
    }

    const fetchSequences = async (id: string) : Promise<Sequence[]> => {
        const resp: SequenceRaw[] = await Call(`/campaigns/${id}/sequences`)
        const sequences = parseSequences(resp)
        updateSequences(id, sequences);
        return sequences
    }

    const updateSequences = (id: string, sequences: Sequence[]) => {
        setCampaigns(bef => bef ? bef.map((c) => c.id === id ? {
            ...c,
            sequences,
        }:c):null);
    }

    const closeNew = () => {
        setError("");
        setAdd(false);
    }

    return (
        <CampaignContext.Provider value={{campaigns, loading, max, getCampaigns, GetCampaigns, GetCampaignsQ, updateCampaign, getCampaign, newCampaign: setAdd, fetchSequences, updateSequences}}>
            {children}
            <div className={`fixed inset-0 z-100 bg-slate-950/45 flex justify-center items-center transition ${add ? "opacity-100 visible":"opacity-0 invisible"}`}>
                <div className={`bg-white duration-200 absolute top-0 right-0 shadow-md py-10 px-10 md:py-14 md:px-20 w-[calc(99%-0px)] h-full ml-auto rounded-l-4xl overflow-y-scroll no-scrollbar transition ${add ? "opacity-100 visible translate-x-0":"opacity-0 invisible translate-x-[20%]"}`}>
                    <div className="absolute top-10 right-10 z-101 text-slate-500 hover:text-slate-400 transition cursor-pointer" onClick={closeNew}>
                        <RiCloseLine className="w-5"/>
                    </div>
                    <h1 className="text-5xl text-slate-600 font-bold font-inter mb-9 mr-4">New Campaign</h1>
                    <p className="text-xl text-slate-400 font-inter max-w-4xl mb-9">Create a brand-new email campaign to reach your audience, deliver targeted content, and track engagement metrics in real-time.</p>
                    <div className="max-w-2xl space-y-6 mb-8">
                        <div>
                            <MiniTitle>Name</MiniTitle>
                            <Input 
                                placeholder="My new campaign"
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                            />
                        </div>
                        <div>
                            <MiniTitle>Description</MiniTitle>
                            <TextArea 
                                placeholder="Targeted for Europe, US"
                                value={description}
                                onChange={(e) => setDescription(e.target.value)}
                            />
                        </div>
                    </div>
                    <div className="flex gap-x-4 gap-y-2 flex-wrap mb-4">
                        <button 
                            className={`ripple px-3 py-2 text-lg w-36 flex justify-center items-center cursor-pointer text-slate-50 transition bg-blue-500 ${!newLoad && "hover:bg-blue-600"} rounded-lg`}
                            onClick={newCampaign}
                        >
                            {newLoad ? <Loading className="h-5"/>:"Save Changes"}
                        </button>
                        <button 
                            className="ripple px-3 py-2 text-lg cursor-pointer text-slate-600 transition bg-slate-100 hover:bg-slate-200 rounded-lg"
                            onClick={() => {
                                setName("");
                                setDescription("");
                            }}
                            >
                            Reset
                        </button>
                    </div>
                    <p className="text-red-600">{error}</p>
                </div>
            </div>
        </CampaignContext.Provider>
    )
}

export function useCampaign() {
  return useContext(CampaignContext);
}

export default CampaignProvider;