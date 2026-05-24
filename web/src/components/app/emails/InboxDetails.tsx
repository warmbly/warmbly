import type Inbox from '@/lib/api/models/app/emails/Inbox';
import { AnimatePresence, motion } from 'framer-motion';
import React from "react";
import DefaultSettings from './details/DefaultSettings';
import { RiCloseLine } from '@remixicon/react';
import { Loading } from '@/components/loader';
import { twColors } from 'tailwindv4-colors';

export default function InboxDetails({
    emails,
    view,
    setView,
}: {
    emails: Inbox[] | null,
    view: string,
    setView: React.Dispatch<React.SetStateAction<string>>
}) {
    const preview = emails?.find((e) => e.id === view)
    const [activeTab, setActiveTab] = React.useState("tab1");
    const [dataLoad, setDataLoad] = React.useState<boolean>(false);
    const [newData, setNewData] = React.useState<Inbox | null>(null)
    const submitRef = React.useRef<() => Promise<void>>(null)
    const [changed, setChanged] = React.useState<boolean>(false);

    React.useEffect(() => {
        if (preview) {
            setNewData(preview)
        }
    }, [preview])

    const [mouseDownOnButton, setMouseDownOnButton] = React.useState(false);
    const handleMouseDown = () => setMouseDownOnButton(true);
    const handleMouseUp = () => {
        if (mouseDownOnButton) {
            setView("")
        }
        setMouseDownOnButton(false);
    };

    const tabData = {
        ...(preview && newData && {
            tab1: {
                title: "Default Settings",
                content: <DefaultSettings preview={preview} setNewData={setNewData} newData={newData} setChanged={setChanged} setLoad={setDataLoad} submitRef={submitRef} />,
            },
        }),
    };

    return <>
        <div onMouseDown={handleMouseDown} onMouseUp={handleMouseUp} className={`fixed inset-0 z-100 flex justify-end bg-slate-950/45 transition ${preview ? "visible opacity-100" : "invisible opacity-0"}`}>
            <div onMouseDown={(e) => e.stopPropagation()} onMouseUp={(e) => e.stopPropagation()} className={`flex flex-col bg-white relative w-200 max-w-[95%] h-full transition-transform ${preview ? "translate-x-0" : "translate-x-100"}`}>
                {preview && <>
                    <div className='overflow-y-scroll p-10 grow'>
                        <div className='flex justify-between gap-10'>
                            <p className='text-gray-400 mb-3 break-all'>ID: {preview.id}</p>
                            <div onMouseDown={handleMouseDown} onMouseUp={handleMouseUp} className='text-gray-300 hover:text-gray-400 cursor-pointer'>
                                <RiCloseLine className='w-5' />
                            </div>
                        </div>
                        <h1 className='text-slate-600 font-medium text-lg break-all mb-5'>{preview.email}</h1>
                        <div className="flex space-x-4 border-b border-gray-200 mb-4">
                            {Object.keys(tabData).map((key) => (
                                <button
                                    key={key}
                                    onClick={() => setActiveTab(key)}
                                    className={`pb-2 px-4 font-medium cursor-pointer border-b-2 transition ${activeTab === key ? "border-blue-500 text-blue-600" : "border-transparent text-gray-500 hover:text-gray-700"}`}
                                >
                                    {tabData[key as keyof typeof tabData]?.title}
                                </button>
                            ))}
                        </div>

                        <div className="mt-4">{tabData[activeTab as keyof typeof tabData]?.content}</div>
                    </div>
                    <div
                        className='bg-white relative shrink-0 py-3 px-10 border-t shadow-xl border-gray-200 bottom-0 left-0 w-full h-17 flex gap-2'
                    >
                        <button
                            onClick={async () => {
                                if (submitRef.current) {
                                    await submitRef.current();
                                }
                            }}
                            className='ripple w-46 cursor-pointer text-blue-500 transition bg-blue-200 hover:bg-blue-300 px-8 py-2 rounded-md'
                        >
                            {dataLoad ? <Loading className='h-5' color={twColors.blue[500]} /> : "Save Changes"}
                        </button>
                        <button
                            onClick={() => setView("")}
                            className='ripple cursor-pointer text-slate-500 transition bg-gray-200 hover:bg-gray-300 px-8 py-2.5 rounded-md'
                        >
                            Cancel
                        </button>
                        <AnimatePresence>
                            {!changed && (
                                <motion.div
                                    className='absolute inset-0 bg-white/50 cursor-not-allowed'
                                    transition={{ type: "spring", duration: 0.1, bounce: 0.3 }}
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                />
                            )}
                        </AnimatePresence>
                    </div>
                </>}
            </div>
        </div>
    </>
}
