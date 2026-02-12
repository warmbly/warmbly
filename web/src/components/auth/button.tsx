import React from "react";
import { Loading } from "../loader";

export default function AuthButton({ children, loading }: { children: React.ReactNode, loading: boolean }) {
    return (
        <button
            type="submit"
            disabled={loading}
            className="w-full h-11 rounded-lg bg-gradient-to-b from-sky-500 to-sky-600 text-white font-semibold text-[15px] shadow-[0_1px_2px_rgba(0,0,0,0.06),0_4px_12px_rgba(14,165,233,0.35)] hover:shadow-[0_1px_2px_rgba(0,0,0,0.06),0_8px_20px_rgba(14,165,233,0.45)] active:shadow-[0_1px_2px_rgba(0,0,0,0.06),0_2px_6px_rgba(14,165,233,0.25)] transition-shadow duration-200 disabled:opacity-50 disabled:pointer-events-none cursor-pointer flex items-center justify-center"
        >
            {!loading ? children : (
                <Loading className="!w-5 h-5 text-white" />
            )}
        </button>
    )
}
