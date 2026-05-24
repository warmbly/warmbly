import { Outlet } from "react-router-dom";

const CARD = "bg-white/95 backdrop-blur-sm rounded-2xl border border-sky-200/40 shadow-[0_8px_40px_-12px_rgba(56,189,248,0.15),0_4px_25px_-5px_rgba(0,0,0,0.07)] p-7 sm:p-8 md:p-10";

export default function RegisterLayout() {
    return (
        <div className="animate-card-float">
            <div className={CARD}>
                <Outlet />
            </div>
        </div>
    );
}
