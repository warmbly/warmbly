// The card both halves of a portal sign-in render: the agency's sign-in page around a title and the body.

import type React from "react";
import { AlertCircleIcon } from "lucide-react";

import PortalSignIn from "@/components/shared/PortalSignIn";
import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { usePortalScope } from "@/hooks/usePortalScope";

export function PortalCard({ brand, title, children }: { brand?: PublicBrand; title: string; children: React.ReactNode }) {
    const scope = usePortalScope(brand);
    const body = (
        <div className="text-center">
            <h1 className="text-[20px] font-semibold tracking-tight text-slate-900">{title}</h1>
            {children}
        </div>
    );
    // A link that names no portal has no brand to show.
    if (!brand) {
        return (
            <div className="min-h-dvh grid place-items-center bg-slate-50 px-4">
                <div className="w-full max-w-sm bg-white rounded-xl border border-slate-200 p-6 shadow-sm">{body}</div>
            </div>
        );
    }
    return (
        <PortalSignIn brand={brand} scope={scope}>
            {body}
        </PortalSignIn>
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
