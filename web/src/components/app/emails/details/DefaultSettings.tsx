import type Inbox from "@/lib/api/models/app/emails/Inbox";
import EmailEditor from "../../EmailEditor";

export default function DefaultSettings({
    preview,
    newData,
    setNewData,
    setChanged,
    setLoad,
    submitRef
}: {
    preview: Inbox,
    newData: Inbox,
    setNewData: React.Dispatch<React.SetStateAction<Inbox | null>>,
    setChanged: React.Dispatch<React.SetStateAction<boolean>>,
    setLoad: React.Dispatch<React.SetStateAction<boolean>>,
    submitRef: React.RefObject<(() => Promise<void>) | null>
}
) {
    const inbox = useInbox();
    const { showError } = useError();
    React.useEffect(() => {
        if (
            newData.name !== preview.name ||
            newData.signature_html !== preview.signature_html ||
            newData.signature_plain !== preview.signature_plain ||
            newData.signature_sync !== preview.signature_sync ||
            newData.signature_code !== preview.signature_code ||
            newData.tags !== preview.tags ||
            newData.campaign_limit !== preview.campaign_limit ||
            newData.min_wait_time !== preview.min_wait_time ||
            newData.reply_to !== preview.reply_to ||
            newData.warmup_base !== preview.warmup_base ||
            newData.warmup_increase !== preview.warmup_increase ||
            newData.warmup_max !== preview.warmup_max ||
            newData.warmup_reply_rate !== preview.warmup_reply_rate
        ) {
            setChanged(true)
        } else {
            setChanged(false)
        }
    }, [preview, newData])

    React.useEffect(() => {
        setNewData(preview)
    }, [preview])

    const [trackLoad, setTrackLoad] = React.useState<boolean>(false);

    submitRef.current = async () => {
        try {
            setLoad(true);
            const data = {
                ...(newData.name !== preview.name && { name: newData.name }),

                ...(newData.signature_plain !== preview.signature_plain && { signature_plain: newData.signature_plain }),
                ...(newData.signature_html !== preview.signature_html && { signature_html: newData.signature_html }),
                ...(newData.signature_sync !== preview.signature_sync && { signature_sync: newData.signature_sync }),
                ...(newData.signature_code !== preview.signature_code && { signature_code: newData.signature_code }),

                ...(newData.campaign_limit !== preview.campaign_limit && { campaign_limit: newData.campaign_limit }),
                ...(newData.min_wait_time !== preview.min_wait_time && { min_wait_time: newData.min_wait_time }),
                ...(newData.reply_to !== preview.reply_to && { reply_to: newData.reply_to }),

                ...(newData.warmup_base !== preview.warmup_base && { warmup_base: newData.warmup_base }),
                ...(newData.warmup_max !== preview.warmup_max && { warmup_max: newData.warmup_max }),
                ...(newData.warmup_increase !== preview.warmup_increase && { warmup_increase: newData.warmup_increase }),
                ...(newData.warmup_reply_rate !== preview.warmup_reply_rate && { warmup_reply_rate: newData.warmup_reply_rate }),

                ...(newData.tags !== preview.tags && { tags: newData.tags }),
            }
            console.log(data, newData.signature_html)
            const resp: InboxRaw = await Call(`/emails/${preview.id}`, "PATCH", data)
            const n = parseInbox(resp)
            inbox?.changeAddress(n)
        } catch (err) {
            if (err instanceof APIError) {
                showError(err.message, err.body.message)
            } else {
                showError("Client Error", `${err}`)
            }
        } finally {
            setLoad(false);
        }
    };

    return (<>
        <div className='grid gap-2'>
            {preview.provider === "smtp_imap" && (
                <>
                    <div>
                        <Head icon={<RiUser3Line className='w-5 h-5' />}>Sender Profile</Head>
                        <SubTitle>Full Name</SubTitle>
                        <MiniInput value={newData.name} placeholder='"First Name" "Last Name"' onChange={(e) => setNewData(bef => bef ? ({ ...bef, name: e.target.value }) : null)} />
                    </div>
                </>
            )}
            <div>
                <Head icon={<RiPenNibLine className='w-5 h-5' />}>Signature</Head>
                <div className='overflow-x-scroll sm:overflow-x-visible pb-1'>
                    <EmailEditor
                        id='inbox-edit-sync'
                        htmlText={newData.signature_html}
                        setHtmlText={(v) => {
                            setNewData((prev) => prev ? ({ ...prev, signature_html: v }) : null);
                        }}
                        plainText={newData.signature_plain}
                        setPlainText={(v) => {
                            setNewData((prev) => prev ? ({ ...prev, signature_plain: v }) : null);
                        }}
                        sync={newData.signature_sync}
                        setSync={(v) => {
                            setNewData((prev) => prev ? ({ ...prev, signature_sync: v }) : null);
                        }}
                        code={newData.signature_code}
                        setCode={(v) => {
                            setNewData((prev) => prev ? ({ ...prev, signature_code: v }) : null);
                        }}
                    />
                </div>
            </div>
            <div>
                <Head icon={<RiPriceTag3Line className='w-5 h-5' />}>Tags</Head>
                <TagSelector
                    selected={newData.tags}
                    onAdd={(v) => {
                        setNewData((prev) => prev ? ({ ...prev, tags: [...prev.tags, v] }) : null)
                    }}
                    onRemove={(v) => {
                        setNewData((prev) => prev ? ({ ...prev, tags: prev.tags.filter((t) => t !== v) }) : null)
                    }}
                />
            </div>
            <div>
                <Head icon={<RiSendPlaneLine className='w-5 h-5' />}>Campaigns</Head>
                <div className='grid md:grid-cols-2 gap-5'>
                    <div>
                        <Title>Daily Campaign Limit</Title>
                        <SubTitle>Daily sending limit</SubTitle>
                        <div className='flex gap-3 items-center'>
                            <div className='max-w-30'>
                                <MiniNumberInput placeholder='30' value={newData.campaign_limit} onChange={(e) => setNewData(bef => bef ? ({ ...bef, campaign_limit: e.target.valueAsNumber }) : null)} />
                            </div>
                            <span className='text-slate-600 font-inter text-sm'>email(s)</span>
                        </div>
                    </div>
                    <div>
                        <Title>Minimum Wait Time</Title>
                        <SubTitle>Minimum time gap between emails</SubTitle>
                        <div className='flex gap-3 items-center'>
                            <div className='max-w-30'>
                                <MiniNumberInput placeholder='10' value={newData.min_wait_time} onChange={(e) => setNewData(bef => bef ? ({ ...bef, min_wait_time: e.target.valueAsNumber }) : null)} />
                            </div>
                            <span className='text-slate-600 font-inter text-sm'>minute(s)</span>
                        </div>
                    </div>
                    <div>
                        <SubTitle>Reply-to</SubTitle>
                        <MiniInput placeholder='support@example.com' value={newData.reply_to} onChange={(e) => setNewData(bef => bef ? ({ ...bef, reply_to: e.target.value }) : null)} />
                    </div>
                </div>
            </div>
            <div>
                <Head icon={<RiMeteorLine className='w-5 h-5' />}>
                    Tracking Domain
                    <span className='bg-blue-500 uppercase text-white text-sm py-1 px-2 rounded-md tracking-widest'>Must-have</span>
                </Head>
                <SubTitle>Track open rates, click rates through your own domain</SubTitle>
                <div className='grid gap-2 p-4 rounded-md bg-gray-100 mb-4'>
                    <div><b>Record Type</b>: CNAME</div>
                    <div><b>Host</b>: prox</div>
                    <div><b>Value</b>: {TRACKING_DOMAIN}</div>
                </div>
                <SubTitle>Tracking domain:</SubTitle>
                <MiniInput placeholder='prox.yourdomain.com' value={newData.tracking_domain} onChange={(e) => setNewData(bef => bef ? ({ ...bef, tracking_domain: e.target.value }) : null)} />
                <button
                    className={`ripple flex items-center justify-center h-10 w-30 transition duration-200 ${preview.tracking_domain === newData.tracking_domain ? "opacity-30 cursor-not-allowed" : "hover:bg-blue-200 cursor-pointer"} bg-blue-100 text-blue-500 rounded-lg mt-5`}
                    onClick={() => setTrackLoad(true)}
                >
                    {trackLoad ? (<Loading className='h-5' color={twColors.blue[500]} />) : "Check Status"}
                </button>
            </div>
            <div>
                <Head icon={<RiFireLine className='w-5 h-5' />}>Email Warmup</Head>
                <div className='grid md:grid-cols-2 gap-y-5 gap-x-15'>
                    <div>
                        <Title>Warmup Start</Title>
                        <SubTitle>Starting amount per day</SubTitle>
                        <div className='flex gap-3 items-center'>
                            <div className='max-w-30'>
                                <MiniNumberInput placeholder='30' value={newData.warmup_base} onChange={(e) => setNewData(bef => bef ? ({ ...bef, warmup_base: e.target.valueAsNumber }) : null)} />
                            </div>
                            <span className='text-slate-600 font-inter text-sm'>email(s)</span>
                        </div>
                    </div>
                    <div>
                        <Title>Daily Increase</Title>
                        <SubTitle>Warmup limit increase</SubTitle>
                        <div className='flex gap-3 items-center'>
                            <div className='max-w-30'>
                                <MiniNumberInput placeholder='10' value={newData.warmup_increase} onChange={(e) => setNewData(bef => bef ? ({ ...bef, warmup_increase: e.target.valueAsNumber }) : null)} />
                            </div>
                            <span className='text-slate-600 font-inter text-sm'>minute(s)</span>
                        </div>
                    </div>
                    <div>
                        <Title>Reply Rate %</Title>
                        <SubTitle>Default: 30</SubTitle>
                        <MiniNumberInput placeholder='30' value={newData.warmup_reply_rate} onChange={(e) => setNewData(bef => bef ? ({ ...bef, warmup_reply_rate: e.target.valueAsNumber }) : null)} />
                    </div>
                    <div>
                        <Title>Daily Warmup limit</Title>
                        <SubTitle>Max emails per day</SubTitle>
                        <div className='flex gap-3 items-center'>
                            <div className='max-w-30'>
                                <MiniNumberInput placeholder='10' value={newData.warmup_max} onChange={(e) => setNewData(bef => bef ? ({ ...bef, warmup_max: e.target.valueAsNumber }) : null)} />
                            </div>
                            <span className='text-slate-600 font-inter text-sm'>minute(s)</span>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </>)
}
