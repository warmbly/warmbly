import React from "react";
import { ADDED, ADDING, APP_URL, GOOGLE_BOX_AUTH, OUTLOOK_BOX_AUTH, PopupCenter } from "@/lib/information";
import { RiDownloadLine, RiInbox2Line, RiMailLine, RiMailSendLine, RiUploadCloud2Line } from "@remixicon/react";
import { Google, Logo, Outlook } from "@/components/svg";
import DefaultHref from "@/components/default-link";
import CopyNote from "@/components/app/note";
import { Input, InputSecret } from "@/components/input";
import Papa, { type ParseResult } from 'papaparse';
import { AnimatePresence, motion } from "framer-motion"
import { useUserProfile } from "@/hooks/context/user";
import type AddEmail from "@/lib/api/models/app/emails/AddEmail";
import toast from "react-hot-toast";
import useAddEmail from "@/lib/api/hooks/app/emails/useAddEmail";
import useAddEmailBulk from "@/lib/api/hooks/app/emails/useAddEmailBulk";
import { useQueryClient } from "@tanstack/react-query";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import AddBox from "../emails/add/AddBox";
import AddBoxFeature from "../emails/add/AddBoxFeature";
import AddBoxFeatures from "../emails/add/AddBoxFeatures";
import AddBoxTop from "../emails/add/AddBoxTop";
import AddBoxTopTitle from "../emails/add/AddBoxTopTitle";
import AddBoxTopBack from "../emails/add/AddBoxTopBack";
import AddBoxStep from "../emails/add/AddBoxStep";
import AddBoxStepButton from "../emails/add/AddBoxStepButton";

export interface EmailEntry {
    name: string;
    email: string;
    imap: {
        username: string;
        password: string;
        host: string;
        port: number;
    };
    smtp: {
        username: string;
        password: string;
        host: string;
        port: number;
    };
}

function Label({ htmlFor, children }: { htmlFor: string, children: React.ReactNode }) {
    return <label htmlFor={htmlFor} className="block mb-1 text-md font-sans font-bold text-gray-600">
        {children}
    </label>
}

function Popup({ children, value }: { children: React.ReactNode, value: boolean }) {
    return (
        <AnimatePresence>
            {value &&
                <motion.div
                    key="popup"
                    initial={{ opacity: 0, x: 80 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: 80 }}
                    transition={{ duration: 0.3, ease: "easeOut" }}
                    className="bg-white absolute top-0 right-0 shadow-md p-7 
                        w-[calc(99%-0px)] h-full ml-auto rounded-l-4xl 
                        overflow-y-scroll no-scrollbar
                      "
                >
                    {children}
                </motion.div>
            }
        </AnimatePresence>
    )
}

export default function AddEmailModal() {
    const user = useUserProfile();

    const queryClient = useQueryClient();
    const addEmail = useAddEmail();
    const addEmailBulk = useAddEmailBulk();

    const [provider, setProvider] = React.useState<string>("")
    const [name, setName] = React.useState<string>("");
    const [email, setEmail] = React.useState<string>("");

    const [smtpUsername, setSmtpUsername] = React.useState<string>("");
    const [smtpPassword, setSmtpPassword] = React.useState<string>("");
    const [smtpHost, setSmtpHost] = React.useState<string>("");
    const [smtpPort, setSmtpPort] = React.useState<string>("");

    const [imapUsername, setImapUsername] = React.useState<string>("");
    const [imapPassword, setImapPassword] = React.useState<string>("");
    const [imapHost, setImapHost] = React.useState<string>("");
    const [imapPort, setImapPort] = React.useState<string>("");

    const [rows, setRows] = React.useState<Record<string, string>[]>([]);

    const [step, setStep] = React.useState<number>(0);
    const [loading, setLoading] = React.useState<boolean>(false);

    const interpolate = (template: string, row: Record<string, string>): string =>
        template.replace(/\{\{(\w+)\}\}/g, (_, col) => row[col.toLowerCase()] || '');

    const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        if (!file) return;

        Papa.parse(file, {
            header: true,
            skipEmptyLines: true,
            transformHeader: h => h.toLowerCase(),
            complete: (result: ParseResult<Record<string, string>>) => {
                setRows(result.data);
            },
        });
    };

    const GetRows = (): EmailEntry[] => {
        const entries: EmailEntry[] = rows.map(row => ({
            name: interpolate(name, row),
            email: interpolate(email, row),
            imap: {
                username: interpolate(imapUsername, row),
                password: interpolate(imapPassword, row),
                host: interpolate(imapHost, row),
                port: Number(interpolate(imapPort, row)),
            },
            smtp: {
                username: interpolate(smtpUsername, row),
                password: interpolate(smtpPassword, row),
                host: interpolate(smtpHost, row),
                port: Number(interpolate(smtpPort, row)),
            },
        }));
        return entries
    };

    const GetSimpleEmail = (): AddEmail => {
        const entry: AddEmail = {
            name: name,
            email: email,
            imap: {
                username: imapUsername,
                password: imapPassword,
                host: imapHost,
                port: Number(imapPort)
            },
            smtp: {
                username: smtpUsername,
                password: smtpPassword,
                host: smtpHost,
                port: Number(smtpPort),
            },
        }
        return entry
    };

    async function SubmitCustom() {
        if (loading) return;
        setLoading(true)
        try {
            await toast.promise(
                addEmail.mutateAsync(GetSimpleEmail()),
                {
                    loading: ADDING,
                    success: ADDED,
                    error: (err: AppError) => buildError(err),
                }
            )
            user?.setAddEmail(false)
        } finally {
            setLoading(false)
        }
    }

    async function SubmitBulk() {
        if (!loading) return;
        setLoading(true);
        try {
            const data = GetRows();
            const chunk = data.slice(0, 30);

            await toast.promise(
                addEmailBulk.mutateAsync(chunk),
                {
                    loading: `Processing ${chunk.length} emails...`,
                    success: "Emails successfully added.",
                    error: (err: AppError) => buildError(err),
                }
            )

            setRows(prev => prev.slice(30))
        } finally {
            setLoading(false)
        }
    }

    React.useEffect(() => {
        const receiveMessage = async (event: MessageEvent) => {
            if (event.origin !== APP_URL) return;

            if (event.data?.type === 'address') {
                queryClient.invalidateQueries({
                    queryKey: ["emails", "list"]
                })
            }
        };

        window.addEventListener('message', receiveMessage);

        return () => {
            window.removeEventListener('message', receiveMessage);
        };
    }, [queryClient]);

    return (
        <AnimatePresence>
            {user?.addEmail &&
                <motion.div
                    key="add-email-modal"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.25 }}
                    className="fixed inset-0 z-[100] bg-slate-950/45 flex justify-center items-center"
                >
                    <Popup value>
                        <AddBoxTopBack onClick={() => user?.setAddEmail(false)}>Add New Email Address</AddBoxTopBack>
                        <div className="flex flex-col justify-center py-20">
                            <h1 className="text-center font-poppins text-xl mb-10">Select Your Email Provider</h1>
                            <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-10">
                                <AddBox>
                                    <div>
                                        <AddBoxTop>
                                            <Google className="w-10" />
                                            <AddBoxTopTitle>Gmail / G-Suite</AddBoxTopTitle>
                                        </AddBoxTop>
                                        <AddBoxFeatures>
                                            <AddBoxFeature>Uses Gmail API (not SMTP)</AddBoxFeature>
                                            <AddBoxFeature>Highest deliverability for Gmail accounts</AddBoxFeature>
                                            <AddBoxFeature>Secure OAuth 2.0 authentication</AddBoxFeature>
                                            <AddBoxFeature>Real-time email sending & tracking</AddBoxFeature>
                                            <AddBoxFeature>Trusted by Google – avoids spam filters</AddBoxFeature>
                                        </AddBoxFeatures>
                                    </div>
                                    <div onClick={() => setProvider("google")} className="cursor-pointer block mt-8 bg-linear-to-r from-blue-500 to-indigo-500 py-2.5 hover:brightness-95 transition rounded-full text-center text-white">
                                        Continue
                                    </div>
                                </AddBox>
                                <AddBox>
                                    <div>
                                        <AddBoxTop>
                                            <Outlook className="w-10" />
                                            <AddBoxTopTitle>Outlook / Office 365 (SMTP/IMAP)</AddBoxTopTitle>
                                        </AddBoxTop>
                                        <AddBoxFeatures>
                                            <AddBoxFeature>Compatible with Outlook.com & Microsoft 365</AddBoxFeature>
                                            <AddBoxFeature>SMTP setup for sending emails</AddBoxFeature>
                                            <AddBoxFeature>IMAP for inbox syncing</AddBoxFeature>
                                            <AddBoxFeature>Supports App Passwords or Basic Auth</AddBoxFeature>
                                            <AddBoxFeature>Email open, reply & bounce tracking</AddBoxFeature>
                                        </AddBoxFeatures>
                                    </div>
                                    <div onClick={() => setProvider("microsoft")} className="cursor-pointer block mt-8 bg-linear-to-r from-blue-500 to-indigo-500 py-2.5 hover:brightness-95 transition rounded-full text-center text-white">
                                        Continue
                                    </div>
                                </AddBox>
                                <AddBox>
                                    <div>
                                        <AddBoxTop>
                                            <Logo className="w-10 -translate-y-1" />
                                            <AddBoxTopTitle>Other (SMTP / IMAP)</AddBoxTopTitle>
                                        </AddBoxTop>
                                        <AddBoxFeatures>
                                            <AddBoxFeature>Connect any email provider</AddBoxFeature>
                                            <AddBoxFeature>Manual SMTP & IMAP configuration</AddBoxFeature>
                                            <AddBoxFeature>Custom ports, encryption & auth settings</AddBoxFeature>
                                            <AddBoxFeature>Use with personal or business domains</AddBoxFeature>
                                            <AddBoxFeature>Full email delivery tracking</AddBoxFeature>
                                        </AddBoxFeatures>
                                    </div>
                                    <div onClick={() => setProvider("custom")} className="cursor-pointer block mt-8 bg-linear-to-r from-slate-400 to-gray-400 py-2.5 hover:brightness-95 transition rounded-full text-center text-white">
                                        Continue
                                    </div>
                                </AddBox>
                            </div>
                        </div>
                    </Popup>
                    <Popup value={provider === "google"}>
                        <AddBoxTopBack onClick={() => setProvider("")}>Gmail / G-Suite Email Setup</AddBoxTopBack>
                        <div className="grid md:grid-cols-2 gap-10 my-20">
                            <div className="grid gap-2">
                                <AddBoxStep step={1}>Go to your <DefaultHref href="https://admin.google.com/u/1/ac/owl/list?tab=configuredApps">Google Workspace Admin Panel</DefaultHref>.</AddBoxStep>
                                <AddBoxStep step={2}>Click "Configure new app".</AddBoxStep>
                                <AddBoxStep step={3}>
                                    Use the following Client-ID to search for <b>Warmblybox</b>:
                                    <CopyNote>212579830838-oe9ou4dvija0lea1lurg7iup631houg8.apps.googleusercontent.com</CopyNote>
                                </AddBoxStep>
                                <AddBoxStep step={4}>Allow Warmbly to access your Google Workspace.</AddBoxStep>
                            </div>
                            <div>
                                <h1 className="text-xl font-bold text-gray-600 mb-3">Authorize Your Account</h1>
                                <p className="text-gray-600 mb-5 text-lg">To enable access, authorization is required once per Google Workspace domain – as long as the user follows the setup steps.
                                    If the steps are not followed, authorization may be required for each individual account.
                                    If you’re using a personal Gmail account, you doesn't have to follow the steps.</p>
                                <div onClick={() => PopupCenter(GOOGLE_BOX_AUTH, "Google Email Authorization")} className="bg-blue-500 cursor-pointer text-lg text-gray-50 w-fit px-5 py-2.5 rounded-xl hover:brightness-95">Connect Your Account</div>
                            </div>
                        </div>
                    </Popup>
                    <Popup value={provider === "microsoft"}>
                        <AddBoxTopBack onClick={() => setProvider("")}>Outlook / Office 365 Email Setup</AddBoxTopBack>
                        <div className="grid md:grid-cols-2 gap-10 my-20">
                            <div className="grid gap-2">
                                <AddBoxStep step={1}>Enable SMTP access for your Microsoft account.</AddBoxStep>
                                <AddBoxStep step={2}>
                                    <b>Purchased from Microsoft:</b><br />
                                    1. Go to <DefaultHref href="https://admin.microsoft.com/homepage">Microsoft Admin Center</DefaultHref>.<br />
                                    2. Open <DefaultHref href="https://admin.microsoft.com/users">Active Users</DefaultHref><br />
                                    3. In the side menu, go to the Mail tab, then select Manage email apps.<br />
                                    4. Make sure both IMAP and Authenticated SMTP are enabled (checked).<br />
                                    5. Click Save changes to apply the settings.<br />
                                    6. Wait approximately one hour, then connect your account to Warmbly.
                                </AddBoxStep>
                                <AddBoxStep step={3}>
                                    <b>Purchased from GoDaddy:</b><br />
                                    1. On your computer, log in to your <DefaultHref href="http://godaddy.com/">GoDaddy</DefaultHref> account.<br />
                                    2. Navigate to the My Products page.<br />
                                    3. Scroll down to the Email & Office section and click Manage All.<br />
                                    4. Locate the user for whom you want to enable SMTP, then click Manage.<br />
                                    5. Scroll down to the Advanced Settings section.<br />
                                    6. Click on SMTP Authentication – the toggle should turn green (enabled).<br />
                                    7. Wait about one hour, then proceed to connect the account to Warmbly.
                                </AddBoxStep>
                            </div>
                            <div>
                                <h1 className="text-xl font-bold text-gray-600 mb-3">Authorize Your Account</h1>
                                <p className="text-gray-600 mb-5 text-lg">To continue, please authorize access to your Microsoft email account.
                                    This step is required to enable email sending through your account.
                                    Authorization needs to be completed for each user individually.
                                    It’s a quick and secure process.</p>
                                <div onClick={() => PopupCenter(OUTLOOK_BOX_AUTH, "Outlook Email Authorization")} className="bg-blue-500 cursor-pointer text-lg text-gray-50 w-fit px-5 py-2.5 rounded-xl hover:brightness-95">Connect Your Account</div>
                            </div>
                        </div>
                    </Popup>
                    <Popup value={provider === "custom"}>
                        <AddBoxTopBack onClick={() => setProvider("")}>Other Email Setup (SMTP/IMAP)</AddBoxTopBack>
                        <div className="max-w-5xl mx-auto overflow-x-hidden flex">
                            <div className={`flex flex-col md:flex-row w-full px-1 shrink-0 gap-10 mx-auto my-10 transition-transform ${step !== 0 || rows.length > 0 ? "-translate-x-[100%]" : "translate-x-0"}`}>
                                <div className="md:grow">
                                    <div className="flex mb-7 gap-7">
                                        <div className="bg-green-400/0 text-gray-800 flex items-center justify-center rounded-xl">
                                            <RiMailLine className="w-8 h-8" />
                                        </div>
                                        <div>
                                            <h1 className="text-3xl font-semibold font-poppins mb-2">Connect Your Account</h1>
                                            <p className="font-poppins text-gray-600">Using SMTP / IMAP</p>
                                        </div>
                                    </div>
                                    <div className="grid gap-2">
                                        <div>
                                            <Label htmlFor="name">Full Name</Label>
                                            <Input value={name} id="name" placeholder="Full Name" onChange={(e) => setName(e.target.value)} />
                                        </div>
                                        <div>
                                            <Label htmlFor="email">Email</Label>
                                            <Input value={email} id="email" placeholder="Email" onChange={(e) => setEmail(e.target.value)} />
                                        </div>
                                    </div>
                                    <div className="grid grid-cols-2 gap-3 mt-5">
                                        <div></div>
                                        <AddBoxStepButton next loading={loading} onClick={() => {
                                            setStep(1)
                                        }} />
                                    </div>
                                </div>
                                <div className="shrink-0 flex justify-center gap-5 md:h-20 md:px-4 items-center text-gray-400 font-poppins">
                                    <hr className="flex-1 border-t border-gray-300 md:hidden" />
                                    <div>or</div>
                                    <hr className="flex-1 border-t border-gray-300 md:hidden" />
                                </div>
                                <div className="grow">
                                    <div className="flex mb-7 gap-7">
                                        <div className="flex items-center justify-center rounded-xl">
                                            <RiDownloadLine className="w-9 h-9" />
                                        </div>
                                        <div>
                                            <h1 className="text-3xl font-semibold font-poppins mb-2">Bulk Import</h1>
                                            <p className="font-poppins text-gray-600">Upload addresses via CSV</p>
                                        </div>
                                    </div>
                                    <p className="text-sm font-medium text-gray-600 font-sans mb-3"><span className="text-red-500">*</span>Paid users only</p>
                                    <label
                                        htmlFor="csv-upload"
                                        className={`
                                    flex items-center gap-2
                                    px-5 py-2.5 border border-gray-300
                                    bg-white hover:bg-gray-100
                                    font-medium text-sm rounded-lg shadow-md
                                    transition-all duration-200 cursor-pointer
                                    focus-within:outline-none focus-within:ring-2 focus-within:ring-offset-2 focus-within:ring-gray-300
                                    `}
                                    >
                                        <RiUploadCloud2Line size={20} />
                                        Upload .CSV
                                        <input
                                            id="csv-upload"
                                            type="file"
                                            accept=".csv"
                                            className="sr-only hidden"
                                            onChange={handleFileChange}
                                        />
                                    </label>
                                </div>
                            </div>
                            <div className={`flex w-full px-1 shrink-0 gap-10 mx-auto my-10 transition-transform ${step === 0 ? "translate-x-[0]" : "-translate-x-[100%]"}`}>
                                <div className="max-w-2xl flex w-full overflow-x-hidden mx-auto">
                                    <div className="w-full px-1 shrink-0 transition-transform" style={{ transform: `translateX(-${(step - 1) * 100}%)` }}>
                                        <div className="flex mb-7 gap-4">
                                            <div className="bg-linear-to-r from-indigo-500/40 to-blue-500/40 text-blue-800 flex items-center justify-center w-10 h-10 rounded-xl">
                                                <RiInbox2Line className="w-5" />
                                            </div>
                                            <div>
                                                <h1 className="text-3xl font-semibold font-poppins mb-2">IMAP Settings</h1>
                                                <p className="font-poppins text-gray-600">Configure incoming emails</p>
                                            </div>
                                        </div>
                                        <div className="grid gap-2">
                                            <div>
                                                <Label htmlFor="imapusername">Username</Label>
                                                <Input value={imapUsername} id="imapusername" placeholder="Username" onChange={(e) => setImapUsername(e.target.value)} />
                                            </div>
                                            <div>
                                                <Label htmlFor="imappassword">Password</Label>
                                                <InputSecret value={imapPassword} id="imappassword" placeholder="Password" onChange={(e) => setImapPassword(e.target.value)} />
                                            </div>
                                            <div className="flex gap-4">
                                                <div className="grow">
                                                    <Label htmlFor="imaphost">Host</Label>
                                                    <Input value={imapHost} id="imaphost" placeholder="Host" onChange={(e) => setImapHost(e.target.value)} />
                                                </div>
                                                <div className="shrink-0 w-30">
                                                    <Label htmlFor="imapport">Port</Label>
                                                    <Input value={imapPort} id="imapport" placeholder="993" onChange={(e) => setImapPort(e.target.value)} />
                                                </div>
                                            </div>
                                        </div>
                                        <div className="grid grid-cols-2 gap-3 mt-5">
                                            <AddBoxStepButton onClick={() => {
                                                setStep(0)
                                            }} />
                                            <AddBoxStepButton next loading={loading} onClick={() => {
                                                setStep(2)
                                            }} />
                                        </div>
                                    </div>
                                    <div className="w-full px-1 shrink-0 transition-transform" style={{ transform: `translateX(-${(step === 2 ? 1 : 0) * 100}%)` }}>
                                        <div className="flex mb-7 gap-4">
                                            <div className="bg-linear-to-r from-indigo-500/40 to-blue-500/40 text-blue-800 flex items-center justify-center w-10 h-10 rounded-xl">
                                                <RiMailSendLine className="w-5" />
                                            </div>
                                            <div>
                                                <h1 className="text-3xl font-semibold font-poppins mb-2">SMTP Provider</h1>
                                                <p className="font-poppins text-gray-600">Set up outgoing emails</p>
                                            </div>
                                        </div>
                                        <div className="grid gap-2">
                                            <div>
                                                <Label htmlFor="smtpusername">Username</Label>
                                                <Input value={smtpUsername} id="smtpusername" placeholder="Username" onChange={(e) => setSmtpUsername(e.target.value)} />
                                            </div>
                                            <div>
                                                <Label htmlFor="smtppassword">Password</Label>
                                                <InputSecret value={smtpPassword} id="smtppassword" placeholder="Password" onChange={(e) => setSmtpPassword(e.target.value)} />
                                            </div>
                                            <div className="flex gap-4">
                                                <div className="grow">
                                                    <Label htmlFor="smtphost">Host</Label>
                                                    <Input value={smtpHost} id="smtphost" placeholder="Host" onChange={(e) => setSmtpHost(e.target.value)} />
                                                </div>
                                                <div className="shrink-0 w-30">
                                                    <Label htmlFor="smtpport">Port</Label>
                                                    <Input value={smtpPort} id="smtpport" placeholder="587" onChange={(e) => setSmtpPort(e.target.value)} />
                                                </div>
                                            </div>
                                        </div>
                                        <div className="grid grid-cols-2 gap-3 mt-5">
                                            <AddBoxStepButton onClick={() => {
                                                setStep(1)
                                            }} />
                                            <AddBoxStepButton next loading={loading} onClick={SubmitCustom} />
                                        </div>
                                    </div>
                                </div>
                            </div>
                            <div className={`flex flex-col w-full px-1 shrink-0 gap-10 mx-auto my-10 transition-transform ${rows.length === 0 ? "translate-x-[0]" : "-translate-x-[200%]"}`}>
                                <div className="w-full overflow-x-scroll">
                                    <table className="preview-table w-full">
                                        <thead className="text-xs text-gray-700 uppercase bg-gray-50">
                                            <tr>
                                                {Object.keys(rows[0] || {}).map(col => (
                                                    <th className="px-6 py-3 text-left" scope="col" key={col}>{col}</th>
                                                ))}
                                                <th className="px-6 py-3 text-left" scope="col">Actions</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {rows.map((row, idx) => (
                                                <tr className="odd:bg-white  even:bg-gray-50 border-gray-200" key={idx}>
                                                    {Object.keys(rows[0] || {}).map(col => (
                                                        <td className="px-6 py-4" key={col}>{row[col]}</td>
                                                    ))}
                                                    <td className="px-6 py-4">
                                                        <button
                                                            className="bg-red-500 px-1.5 py-0.5 text-[15px] rounded-lg text-white cursor-pointer hover:bg-red-600"
                                                            onClick={() => setRows(prev => prev.filter((_, i) => i !== idx))}
                                                        >
                                                            Delete
                                                        </button>
                                                    </td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                </div>
                                <div className="w-full px-1 shrink-0 transition-transform" style={{ transform: `translateX(-${(step - 1) * 100}%)` }}>
                                    <div className="flex mb-7 gap-4">
                                        <div className="bg-linear-to-r from-indigo-500/40 to-blue-500/40 text-blue-800 flex items-center justify-center w-10 h-10 rounded-xl">
                                            <RiInbox2Line className="w-5" />
                                        </div>
                                        <div>
                                            <h1 className="text-3xl font-semibold font-poppins mb-2">IMAP Settings</h1>
                                            <p className="font-poppins text-gray-600">Configure incoming emails</p>
                                        </div>
                                    </div>
                                    <div className="grid gap-2">
                                        <div>
                                            <Label htmlFor="imapusername">Username</Label>
                                            <Input value={imapUsername} id="imapusername" placeholder="{{firstName}} {{lastName}}" onChange={(e) => setImapUsername(e.target.value)} />
                                        </div>
                                        <div>
                                            <Label htmlFor="imappassword">Password</Label>
                                            <Input value={imapPassword} id="imappassword" placeholder="{{password}}" onChange={(e) => setImapPassword(e.target.value)} />
                                        </div>
                                        <div className="flex gap-4">
                                            <div className="grow">
                                                <Label htmlFor="imaphost">Host</Label>
                                                <Input value={imapHost} id="imaphost" placeholder="{{host}}" onChange={(e) => setImapHost(e.target.value)} />
                                            </div>
                                            <div className="shrink-0 w-30">
                                                <Label htmlFor="imapport">Port</Label>
                                                <Input value={imapPort} id="imapport" placeholder="{{port}}" onChange={(e) => setImapPort(e.target.value)} />
                                            </div>
                                        </div>
                                    </div>
                                    <div className="flex mb-7 gap-4 mt-20">
                                        <div className="bg-linear-to-r from-indigo-500/40 to-blue-500/40 text-blue-800 flex items-center justify-center w-10 h-10 rounded-xl">
                                            <RiInbox2Line className="w-5" />
                                        </div>
                                        <div>
                                            <h1 className="text-3xl font-semibold font-poppins mb-2">SMTP Settings</h1>
                                            <p className="font-poppins text-gray-600">Configure incoming emails</p>
                                        </div>
                                    </div>
                                    <div className="grid gap-2">
                                        <div>
                                            <Label htmlFor="smtpusername">Username</Label>
                                            <Input value={smtpUsername} id="smtpusername" placeholder="{{firstName}} {{lastName}}" onChange={(e) => setSmtpUsername(e.target.value)} />
                                        </div>
                                        <div>
                                            <Label htmlFor="smtppassword">Password</Label>
                                            <Input value={smtpPassword} id="smtppassword" placeholder="{{password}}" onChange={(e) => setSmtpPassword(e.target.value)} />
                                        </div>
                                        <div className="flex gap-4">
                                            <div className="grow">
                                                <Label htmlFor="smtphost">Host</Label>
                                                <Input value={smtpHost} id="smtphost" placeholder="{{host}}" onChange={(e) => setSmtpHost(e.target.value)} />
                                            </div>
                                            <div className="shrink-0 w-30">
                                                <Label htmlFor="smtpport">Port</Label>
                                                <Input value={smtpPort} id="smtpport" placeholder="{{port}}" onChange={(e) => setSmtpPort(e.target.value)} />
                                            </div>
                                        </div>
                                    </div>
                                    <div className="grid grid-cols-2 gap-3 mt-5">
                                        <AddBoxStepButton tt="Cancel" onClick={() => {
                                            setRows([])
                                        }} />
                                        <AddBoxStepButton tt='Submit' loading={loading} onClick={SubmitBulk} />
                                    </div>
                                </div>
                            </div>
                        </div>
                    </Popup>
                </motion.div>
            }
        </AnimatePresence>
    );
};

