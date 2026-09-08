// Mobile navigation drawer. Below `md` the sidebar is hidden, so the
// Topbar's hamburger opens this left sheet with the same nav model.
// Closes on a tap (onNavigate), on Escape and on the backdrop (Radix), and
// on any route change so a browser back button never leaves it open.

import { useEffect, useRef } from "react";
import { useLocation } from "react-router-dom";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { AdminBadge } from "./AdminBadge";
import { NavList, SidebarBrand } from "./Sidebar";

interface Props {
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

export function MobileNav({ open, onOpenChange }: Props) {
    const location = useLocation();
    const routeKey = location.pathname + location.search;
    const lastRoute = useRef(routeKey);

    useEffect(() => {
        if (lastRoute.current === routeKey) return;
        lastRoute.current = routeKey;
        onOpenChange(false);
    }, [routeKey, onOpenChange]);

    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent
                side="left"
                showCloseButton
                className="w-72 max-w-[85vw] gap-0 bg-sidebar admin-sidebar-pattern p-0 md:hidden"
            >
                <SheetTitle className="sr-only">Navigation</SheetTitle>
                <SheetDescription className="sr-only">Admin panel sections</SheetDescription>
                <div className="flex h-14 shrink-0 items-center gap-2 px-4 pr-12 border-b border-sidebar-border">
                    <SidebarBrand />
                    <AdminBadge compact />
                </div>
                <nav className="flex-1 overflow-y-auto px-2 py-3 space-y-5">
                    <NavList onNavigate={() => onOpenChange(false)} />
                </nav>
            </SheetContent>
        </Sheet>
    );
}
