import { RiAppleFill } from "@remixicon/react";
import { Google } from "../svg";
import { API_URL, PopupCenter } from "@/lib/information";

export default function ExternalLogin() {
    return (
        <div className="grid grid-cols-2 gap-3">
            <button
                type="button"
                onClick={() => PopupCenter(`${API_URL}/auth/google/login`, "Google Login")}
                className="flex items-center justify-center gap-2.5 h-11 rounded-lg border border-sky-200/70 bg-white text-sm font-medium text-slate-600 hover:bg-sky-50/50 hover:border-sky-300 hover:text-slate-800 transition-colors duration-200 cursor-pointer"
            >
                <Google className="w-4 shrink-0" />
                <span>Google</span>
            </button>
            <button
                type="button"
                onClick={() => PopupCenter(`${API_URL}/auth/apple/login`, "Apple Login")}
                className="flex items-center justify-center gap-2.5 h-11 rounded-lg border border-sky-200/70 bg-white text-sm font-medium text-slate-600 hover:bg-sky-50/50 hover:border-sky-300 hover:text-slate-800 transition-colors duration-200 cursor-pointer"
            >
                <RiAppleFill className="size-4 shrink-0" />
                <span>Apple</span>
            </button>
        </div>
    )
}
