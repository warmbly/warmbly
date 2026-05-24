import { useUserProfile } from "@/hooks/context/user";
import React from "react"
import { useLocation, Link } from "react-router-dom";
import WorkerHealthAlert from "./_components/WorkerHealthAlert";

export default function AdminLayout({ children }: { children: React.ReactNode }) {
    const user = useUserProfile();
    const { pathname } = useLocation();

    const tabData = {
        "Home": "",
        "Workers": "/workers",
        "Credentials": "/credentials",
        "Audit": "/audit",
        "Roles": "/roles",
        "Users": "/users",
        "Plans": "/plans",
    }

    return (
        <div className="md:px-4">
            <div className="mb-5">
                <h1 className="text-slate-600 font-bold font-inter text-2xl mb-4">Administration</h1>
                <p className="text-slate-400 text-sm font-inter">Logged in as: {user ? user.user.email : "Loading..."}</p>
            </div>
            <WorkerHealthAlert />
            <div className="flex space-x-4 pb-3 border-b border-gray-200 mb-4 overflow-x-scroll no-scrollbar">
                {Object.entries(tabData).map((key) => {
                    const fullPath = `/app/admin/${key[1]}`;
                    const isActive = pathname.replaceAll("/", "") === fullPath.replaceAll("/", "");
                    return (
                        <Link
                            key={key[1]}
                            to={fullPath}
                            className={`py-1 px-3 font-medium cursor-pointer rounded-lg transition ${isActive
                                ? "bg-blue-100 text-blue-600"
                                : "hover:bg-gray-100 text-gray-500 hover:text-gray-700"
                                }`}
                        >
                            {key[0]}
                        </Link>
                    );
                })}
            </div>
            {children}
        </div>
    )
}
