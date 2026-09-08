// Cmd/Ctrl+K palette. "Go to" lists every nav item the admin can open;
// typing two or more characters searches users, organizations, mailboxes
// and workers live; "Actions" holds the shortcuts that are not pages.
// Open state lives in a tiny module store so the Topbar button and the
// keyboard shortcut share one palette without a context provider.

import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    Building2,
    ClipboardCopy,
    Mailbox,
    RefreshCw,
    Server,
    User as UserIcon,
} from "lucide-react";
import {
    CommandDialog,
    CommandEmpty,
    CommandGroup,
    CommandInput,
    CommandItem,
    CommandList,
} from "@/components/ui/command";
import { useMe } from "@/hooks/useMe";
import { UPDATE_JOB_KEY, UPDATE_STATE_KEY } from "@/hooks/useUpdateState";
import { AdminPerm, hasAdminPerm } from "@/lib/auth/permissions";
import { searchUsers } from "@/lib/api/client/admin/users";
import { listOrganizations } from "@/lib/api/client/admin/organizations";
import { searchMailboxes } from "@/lib/api/client/admin/mailboxes";
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import { checkForUpdates } from "@/lib/api/client/admin/updates";
import { visibleNavGroups } from "./Sidebar";

// ---- open state -----------------------------------------------------------

let paletteOpen = false;
const listeners = new Set<() => void>();

function setPaletteOpen(next: boolean) {
    if (paletteOpen === next) return;
    paletteOpen = next;
    listeners.forEach((l) => l());
}

export function openCommandPalette() {
    setPaletteOpen(true);
}

function subscribe(listener: () => void) {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

function usePaletteOpen() {
    return useSyncExternalStore(subscribe, () => paletteOpen, () => false);
}

export const IS_MAC =
    typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

// ---- helpers --------------------------------------------------------------

const SEARCH_MIN = 2;
const SEARCH_DEBOUNCE_MS = 250;
const SEARCH_LIMIT = 5;

function useDebounced(value: string, ms: number): string {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const t = window.setTimeout(() => setDebounced(value), ms);
        return () => window.clearTimeout(t);
    }, [value, ms]);
    return debounced;
}

function includes(haystack: string, needle: string): boolean {
    return haystack.toLowerCase().includes(needle.toLowerCase());
}

// heldPermissionNames lists the AdminPerm keys set in the mask, so the copy
// action adapts when bits are added or retired.
function heldPermissionNames(mask: number | undefined): string[] {
    if (typeof mask !== "number") return [];
    return Object.entries(AdminPerm)
        .filter(([, bit]) => (mask & bit) === bit)
        .map(([name]) => name);
}

// ---- component ------------------------------------------------------------

export function CommandPalette() {
    const open = usePaletteOpen();
    const nav = useNavigate();
    const location = useLocation();
    const qc = useQueryClient();
    const { data: me } = useMe();
    const mask = me?.admin_permissions;

    const [query, setQuery] = useState("");
    const debounced = useDebounced(query.trim(), SEARCH_DEBOUNCE_MS);
    const searching = open && debounced.length >= SEARCH_MIN;

    // Cmd/Ctrl+K toggles from anywhere, including inside inputs.
    useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "k") {
                e.preventDefault();
                setPaletteOpen(!paletteOpen);
            }
        }
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, []);

    // A navigation from anywhere (palette item, back button) closes it.
    const routeKey = location.pathname + location.search;
    useEffect(() => {
        setPaletteOpen(false);
    }, [routeKey]);

    // Start every session with an empty query.
    useEffect(() => {
        if (!open) setQuery("");
    }, [open]);

    const canUsers = hasAdminPerm(mask, AdminPerm.ViewUsers);
    const canOrgs = hasAdminPerm(mask, AdminPerm.ViewOrganizations);
    const canWorkers = hasAdminPerm(mask, AdminPerm.ViewWorkers);
    const canUpdates = hasAdminPerm(mask, AdminPerm.ManageSettings);

    const usersQ = useQuery({
        queryKey: ["admin", "palette", "users", debounced],
        queryFn: () => searchUsers({ q: debounced, limit: SEARCH_LIMIT }),
        enabled: searching && canUsers,
        staleTime: 30_000,
    });
    const orgsQ = useQuery({
        queryKey: ["admin", "palette", "organizations", debounced],
        queryFn: () => listOrganizations({ q: debounced, limit: SEARCH_LIMIT }),
        enabled: searching && canOrgs,
        staleTime: 30_000,
    });
    const mailboxesQ = useQuery({
        queryKey: ["admin", "palette", "mailboxes", debounced],
        queryFn: () => searchMailboxes({ q: debounced, limit: SEARCH_LIMIT }),
        enabled: searching && canUsers,
        staleTime: 30_000,
    });
    const workersQ = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        enabled: searching && canWorkers,
        staleTime: 30_000,
    });

    const workers = useMemo(() => {
        if (!searching) return [];
        return (workersQ.data?.data ?? [])
            .filter(
                (w) =>
                    includes(w.name, debounced) ||
                    includes(w.id, debounced) ||
                    includes(w.ip_addr ?? "", debounced) ||
                    includes(w.ssh_host ?? "", debounced),
            )
            .slice(0, SEARCH_LIMIT);
    }, [workersQ.data, debounced, searching]);

    const pages = useMemo(
        () =>
            visibleNavGroups(mask)
                .flatMap((g) => g.items)
                .filter((item) => !query.trim() || includes(item.label, query.trim())),
        [mask, query],
    );

    const checkMut = useMutation({
        mutationFn: checkForUpdates,
        onSuccess: (data) => {
            qc.setQueryData(UPDATE_STATE_KEY, data);
            qc.setQueryData(UPDATE_JOB_KEY, data);
            toast.success(
                data.update_available ? "A newer version is available" : "This instance is up to date",
            );
        },
        onError: (err: Error) => toast.error(err.message || "Could not check for updates"),
    });

    function go(to: string) {
        setPaletteOpen(false);
        nav(to);
    }

    async function copyPermissions() {
        setPaletteOpen(false);
        const names = heldPermissionNames(mask);
        const text = names.length
            ? `${names.join(", ")} (mask ${mask})`
            : "No admin permission bits reported";
        try {
            await navigator.clipboard.writeText(text);
            toast.success("Admin permissions copied");
        } catch {
            toast.error("Could not copy to the clipboard");
        }
    }

    const actions = [
        {
            id: "copy-permissions",
            label: "Copy my admin permissions",
            icon: ClipboardCopy,
            run: copyPermissions,
            show: true,
        },
        {
            id: "check-updates",
            label: "Check for updates",
            icon: RefreshCw,
            run: () => {
                setPaletteOpen(false);
                checkMut.mutate();
            },
            show: canUpdates,
        },
    ].filter((a) => a.show && (!query.trim() || includes(a.label, query.trim())));

    const users = usersQ.data?.data ?? [];
    const orgs = orgsQ.data?.data ?? [];
    const mailboxes = mailboxesQ.data?.data ?? [];
    const loading =
        searching &&
        (usersQ.isFetching || orgsQ.isFetching || mailboxesQ.isFetching || workersQ.isFetching);
    const nothing =
        pages.length === 0 &&
        actions.length === 0 &&
        users.length === 0 &&
        orgs.length === 0 &&
        mailboxes.length === 0 &&
        workers.length === 0;

    return (
        <CommandDialog open={open} onOpenChange={setPaletteOpen} shouldFilter={false}>
            <CommandInput
                placeholder="Go to a page, or search users, organizations, mailboxes and workers"
                value={query}
                onValueChange={setQuery}
            />
            <CommandList>
                {nothing && (
                    <CommandEmpty>
                        {loading
                            ? "Searching"
                            : searching
                              ? "Nothing matches that. Try an email, a name or a worker."
                              : "No page matches that."}
                    </CommandEmpty>
                )}
                {pages.length > 0 && (
                    <CommandGroup heading="Go to">
                        {pages.map((item) => (
                            <CommandItem key={item.to} value={`page:${item.to}`} onSelect={() => go(item.to)}>
                                <item.icon className="size-4" />
                                <span>{item.label}</span>
                                <span className="ml-auto font-mono text-[11px] text-muted-foreground">
                                    {item.to}
                                </span>
                            </CommandItem>
                        ))}
                    </CommandGroup>
                )}
                {users.length > 0 && (
                    <CommandGroup heading="Users">
                        {users.map((u) => (
                            <CommandItem key={u.id} value={`user:${u.id}`} onSelect={() => go(`/users/${u.id}`)}>
                                <UserIcon className="size-4" />
                                <span className="truncate">{u.email}</span>
                                <span className="ml-auto truncate text-[11px] text-muted-foreground">
                                    {[u.first_name, u.last_name].filter(Boolean).join(" ")}
                                </span>
                            </CommandItem>
                        ))}
                    </CommandGroup>
                )}
                {orgs.length > 0 && (
                    <CommandGroup heading="Organizations">
                        {orgs.map((o) => (
                            <CommandItem
                                key={o.id}
                                value={`org:${o.id}`}
                                onSelect={() => go(`/organizations/${o.id}`)}
                            >
                                <Building2 className="size-4" />
                                <span className="truncate">{o.name}</span>
                                <span className="ml-auto truncate text-[11px] text-muted-foreground">
                                    {o.owner_email}
                                </span>
                            </CommandItem>
                        ))}
                    </CommandGroup>
                )}
                {mailboxes.length > 0 && (
                    <CommandGroup heading="Mailboxes">
                        {mailboxes.map((m) => (
                            <CommandItem
                                key={m.id}
                                value={`mailbox:${m.id}`}
                                onSelect={() => go(`/mailboxes?q=${encodeURIComponent(m.email)}`)}
                            >
                                <Mailbox className="size-4" />
                                <span className="truncate">{m.email}</span>
                                <span className="ml-auto truncate text-[11px] text-muted-foreground">
                                    {m.org_name || m.owner_email}
                                </span>
                            </CommandItem>
                        ))}
                    </CommandGroup>
                )}
                {workers.length > 0 && (
                    <CommandGroup heading="Workers">
                        {workers.map((w) => (
                            <CommandItem key={w.id} value={`worker:${w.id}`} onSelect={() => go(`/workers/${w.id}`)}>
                                <Server className="size-4" />
                                <span className="truncate">{w.name}</span>
                                <span className="ml-auto truncate font-mono text-[11px] text-muted-foreground">
                                    {w.ssh_host || w.ip_addr}
                                </span>
                            </CommandItem>
                        ))}
                    </CommandGroup>
                )}
                {actions.length > 0 && (
                    <CommandGroup heading="Actions">
                        {actions.map((a) => (
                            <CommandItem key={a.id} value={`action:${a.id}`} onSelect={a.run}>
                                <a.icon className="size-4" />
                                <span>{a.label}</span>
                            </CommandItem>
                        ))}
                    </CommandGroup>
                )}
            </CommandList>
        </CommandDialog>
    );
}
