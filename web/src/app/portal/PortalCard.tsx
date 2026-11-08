// The card both halves of a portal sign-in render: the agency's mark, a title and the body.

import type React from "react";
import { AlertCircleIcon } from "lucide-react";

import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { safeAccent } from "@/lib/portal";

export function PortalCard({ brand, title, children }: { brand?: PublicBrand; title: string; children: React.ReactNode }) {
    const accent = safeAccent(brand?.accent_color);
    return (
        <div className="min-h-dvh grid place-items-center bg-slate-50 px-4">
            <div className="w-full max-w-sm bg-white rounded-xl border border-slate-200 p-6 shadow-sm text-center">
                {brand && (
                    <div className="mb-4 flex flex-col items-center gap-2">
                        {brand.logo_url ? (
                            <img src={brand.logo_url} alt="" className="h-10 max-w-[160px] object-contain" />
                        ) : (
                            <span
                                className="grid size-10 place-items-center rounded-lg bg-slate-900 text-[15px] font-semibold text-white"
                                style={accent ? { backgroundColor: accent } : undefined}
                                aria-hidden="true"
                            >
                                {brand.name.slice(0, 1).toUpperCase()}
                            </span>
                        )}
                        <span className="text-[12.5px] font-medium text-slate-600">{brand.name}</span>
                    </div>
                )}
                <h1 className="text-[18px] font-semibold text-slate-900">{title}</h1>
                {children}
            </div>
        </div>
    );
}

export function PortalError({ message, action }: { message: string; action?: React.ReactNode }) {
    return (
        <>
            <div className="mx-auto mt-4 w-12 h-12 rounded-xl bg-red-50 grid place-items-center">
                <AlertCircleIcon className="w-6 h-6 text-red-500" />
            </div>
            <p className="text-sm text-slate-500 mt-3">{message}</p>
            {action}
        </>
    );
}
