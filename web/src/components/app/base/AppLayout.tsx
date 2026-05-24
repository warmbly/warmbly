import { Logo } from "@/components/svg";
import { RiArrowRightDoubleLine, RiBarChart2Line, RiCustomerService2Line, RiDatabase2Line, RiInbox2Line, RiInformation2Line, RiListSettingsLine, RiMailSettingsLine, RiMegaphoneLine, RiSecurePaymentLine, RiTeamLine } from "@remixicon/react";
import React from "react";
import { useWindowWidth } from "@/hooks/WindowWidth";
import { useSocket } from "@/hooks/context/socket";
import { isEmailFromOrganization, orgs } from "@/lib/helper";
import { useUserProfile } from "@/hooks/context/user";
import { useLocation } from "react-router-dom";
import PageLink from "./PageLink";
import { Link } from "react-router-dom";
import PageTitle from "./PageTitle";

const IconClass = '-mt-1'

export default function AppLayout({ children }: { children: React.ReactNode }) {
    const socket = useSocket();
    const width = useWindowWidth();
    const { pathname } = useLocation();
    const [side, setSide] = React.useState<boolean>(false);
    const profile = useUserProfile();

    const paths = {
        "emails": {
            title: "Email Addresses",
            icon: <RiMailSettingsLine className={IconClass} />
        },
        "analytics": {
            title: "Analytics",
            icon: <RiBarChart2Line className={IconClass} />
        },
        "contacts": {
            title: "Contacts",
            icon: <RiTeamLine className={IconClass} />
        },
        "campaigns": {
            title: "Campaigns",
            icon: <RiMegaphoneLine className={IconClass} />
        },
        "unibox": {
            title: "Unibox",
            icon: <RiInbox2Line className={IconClass} />
        },
        "help": {
            title: "Customer Service",
            icon: <RiCustomerService2Line className={IconClass} />
        },
        "billing": {
            title: "Billing",
            icon: <RiSecurePaymentLine className={IconClass} />
        },
        ...(profile && isEmailFromOrganization(profile.user.email, orgs)
            ? {
                'admin': {
                    title: "Administration",
                    icon: <RiDatabase2Line className={IconClass} />,
                },
            }
            : {}),
    }

    const title = Object.entries(paths).find((o) => pathname.startsWith(`/app/${o[0]}`))

    return <>
        <div className="flex h-screen w-screen overflow-x-hidden">
            <div className={`${!side ? width < 340 ? "-ml-[95%]" : "-ml-85" : "ml-0"} lg:ml-0 transition-all flex w-85 max-w-[95%] shrink-0 bg-white shadow-md border-r border-gray-200 z-2 py-4 px-10 flex-col justify-between overflow-y-scroll no-scrollbar`}>
                <div>
                    <div className="flex items-center gap-10">
                        <Logo className="w-10" />
                        <h1 className="font-sans font-semibold text-2xl -mb-[9px]">Warmbly.</h1>
                    </div>
                    <div className="flex flex-col w-full mt-10 lg:mt-15">
                        {Object.entries(paths).map((v) => (
                            <PageLink key={v[0]} href={`/app/${v[0]}`}>
                                {v[1].icon}
                                <PageTitle>{v[1].title}</PageTitle>
                            </PageLink>
                        ))}
                    </div>
                </div>
                <div className="mt-[auto]">
                    <PageLink href="/app/account">
                        <RiListSettingsLine className={IconClass} />
                        <PageTitle>Account Settings</PageTitle>
                    </PageLink>
                </div>
            </div>
            <div className={`w-full lg:w-auto lg:grow flex flex-col transition lg:translate-x-0`}>
                <div className="hidden shrink-0 lg:flex h-20 bg-white border-b border-gray-200 shadow-xs items-center px-10 justify-between gap-10">
                    <div>
                        <h1 className="font-bold flex gap-3 items-center">
                            <span className="font-sans text-gray-500 text-lg">Warmbly</span>
                            <span className="font-sans font-sm opacity-50"><RiArrowRightDoubleLine /></span>
                            <span className="font-sans text-gray-700 text-lg">{title ? title[1].title : "Unknown"}</span>
                        </h1>
                    </div>
                    <div className="flex items-center gap-5">
                        <div className="relative flex gap-3 items-center group">
                            <RiInformation2Line className={`w-4 text-gray-400 transition duration-300 ${socket.error ? "opacity-100" : "opacity-0"}`} />
                            <div className={`w-2 h-2 rounded-full ${!socket.isConnected ? "bg-orange-400 animate-pulse" : socket.error ? "bg-red-400" : "bg-green-400 animate-pulse"}`}></div>
                            {socket.error && <div className="absolute px-3 py-2 text-sm rounded-lg bg-gray-50 text-gray-700 border border-gray-300 w-70 right-0 duration-300 top-[100%] transition invisible opacity-0 group-hover:visible group-hover:opacity-100 shadowmd">{socket.message}</div>}
                        </div>
                        <Link to="/app/settings" className="w-10 h-10 text-white bg-gray-400 rounded-full flex items-center justify-center text-lg">
                            {profile?.user.email.charAt(0).toUpperCase()}
                        </Link>
                    </div>
                </div>
                <div className="flex w-full shrink-0 lg:hidden h-16 bg-white border-b border-gray-200 shadow-xs items-center px-6 justify-between gap-10">
                    <div className="flex items-center gap-6">
                        <Logo className="w-8 -mt-1" />
                        <div className="font-sans font-semibold text-[22px] -mb-[3px]">Warmbly.</div>
                    </div>
                    <div className="flex flex-col justify-around w-10 h-8" onClick={() => setSide(true)}>
                        <div className="h-[3px] bg-gray-600 rounded-full"></div>
                        <div className="h-[3px] bg-gray-600 rounded-full"></div>
                        <div className="h-[3px] bg-gray-600 rounded-full"></div>
                    </div>
                </div>
                <div className="grow overflow-y-scroll overflow-x-hidden p-5 lg:p-10">
                    <div className="max-w-7xl mx-auto">
                        {children}
                    </div>
                </div>
                <div className={`lg:hidden fixed bg-gray-900/40 transition inset-0 ${side ? "opacity-100 visible" : "opacity-0 invisible"}`} onClick={() => setSide(false)}></div>
            </div>
        </div >
    </>
}
