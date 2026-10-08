// Labels tab: the workspace's contact labels with a live contact count,
// inline rename, color, create and delete. Clicking a row opens the contact
// list filtered to that category.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseText } from "@/lib/browse-contacts-campaigns";
import { useNavigate } from "@tanstack/react-router";
import { useQueries } from "@tanstack/react-query";
import { CheckIcon, MoreHorizontalIcon, PlusIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, TopbarAction } from "@/components/layout/Page";
import { SearchInput, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useUserProfile } from "@/hooks/context/user";
import { useWriteGuard } from "@/hooks/usePermission";
import useCreateCategory from "@/lib/api/hooks/app/categories/useCreateCategory";
import useDeleteCategory from "@/lib/api/hooks/app/categories/useDeleteCategory";
import useUpdateCategory from "@/lib/api/hooks/app/categories/useUpdateCategory";
import { labelCountQuery } from "@/lib/api/hooks/app/segments";
import type Category from "@/lib/api/models/app/Category";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

const COLORS = ["#0284c7", "#7c3aed", "#db2777", "#dc2626", "#ea580c", "#ca8a04", "#16a34a", "#0d9488", "#475569"];

export default function LabelsPage() {
    const { user } = useUserProfile();
    const write = useWriteGuard("MANAGE_CONTACTS");
    const guarded = (fn: () => void) => () => write.guard(fn)({});
    const create = useCreateCategory();
    const [query, setQuery] = useBrowseState("contacts:labels:query", "", browseText);
    const [creating, setCreating] = React.useState(false);
    const [newTitle, setNewTitle] = React.useState("");

    const categories = React.useMemo(
        () => [...(user.categories ?? [])].sort((a, b) => a.position - b.position),
        [user.categories],
    );
    const list = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        return q ? categories.filter((c) => c.title.toLowerCase().includes(q)) : categories;
    }, [categories, query]);

    // One live count per category through the segment preview, so the number
    // agrees with what a "has any of" condition would match.
    const counts = useQueries({ queries: categories.map((c) => labelCountQuery(c.id)) });
    const countById = new Map<string, number | undefined>();
    categories.forEach((c, i) => countById.set(c.id, counts[i]?.data));

    async function submitCreate() {
        const title = newTitle.trim();
        if (!title || create.isPending) return;
        try {
            await create.mutateAsync(title);
            toast.success(`Created ${title}`);
            setNewTitle("");
            setCreating(false);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <Page>
            <PageTopbar eyebrow="Labels" subtitle="One list for contacts, inbox conversations and forms">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(() => setCreating(true))}>
                    New label
                </TopbarAction>
            </PageTopbar>

            <SectionBar label="All labels" count={list.length}>
                <SearchInput value={query} onChange={setQuery} placeholder="Search labels…" className="w-full sm:w-64" />
            </SectionBar>

            <PageBody>
                {creating && (
                    <form
                        onSubmit={(e) => {
                            e.preventDefault();
                            void submitCreate();
                        }}
                        className="h-11 px-5 flex items-center gap-2 border-b border-slate-200/60 bg-sky-50/40"
                    >
                        <TextInput value={newTitle} onChange={setNewTitle} placeholder="Label name" autoFocus className="w-64" />
                        <button
                            type="submit"
                            disabled={!newTitle.trim() || create.isPending}
                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors disabled:opacity-50"
                        >
                            Create
                        </button>
                        <button
                            type="button"
                            onClick={() => {
                                setCreating(false);
                                setNewTitle("");
                            }}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            Cancel
                        </button>
                    </form>
                )}
                {list.length === 0 ? (
                    <EmptyBlock
                        title={query ? "No labels match" : "No labels yet"}
                        body={
                            query
                                ? "Try a different search."
                                : "Put a label on a contact or an inbox conversation. Create one here, on a contact, in the inbox, or by mapping a column during import."
                        }
                        cta={
                            query ? undefined : (
                                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(() => setCreating(true))}>
                                    New label
                                </TopbarAction>
                            )
                        }
                    />
                ) : (
                    <div>
                        {list.map((c) => (
                            <CategoryRow key={c.id} category={c} count={countById.get(c.id)} />
                        ))}
                    </div>
                )}
            </PageBody>
        </Page>
    );
}

function CategoryRow({ category, count }: { category: Category; count?: number }) {
    const navigate = useNavigate();
    const confirm = useConfirm();
    const write = useWriteGuard("MANAGE_CONTACTS");
    const guarded = (fn: () => void) => () => write.guard(fn)({});
    const update = useUpdateCategory(category.id);
    const remove = useDeleteCategory(category.id);
    const [renaming, setRenaming] = React.useState(false);
    const [title, setTitle] = React.useState(category.title);

    const open = () => navigate({ to: "/app/contacts", search: { category: category.id } });

    async function submitRename() {
        const next = title.trim();
        if (!next || next === category.title) {
            setRenaming(false);
            setTitle(category.title);
            return;
        }
        try {
            await update.mutateAsync({ title: next });
            toast.success("Label renamed");
            setRenaming(false);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function setColor(color: string) {
        if (color === category.color) return;
        try {
            await update.mutateAsync({ color });
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    function askDelete() {
        confirm.show(`Delete the label "${category.title}"? It is removed from every contact and inbox conversation; the contacts themselves are kept.`, async () => {
            try {
                await remove.mutateAsync();
                toast.success("Label deleted");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    return (
        <div
            role="link"
            tabIndex={0}
            onClick={() => {
                if (!renaming) open();
            }}
            onKeyDown={(e) => {
                if (e.key === "Enter" && !renaming) open();
            }}
            className="group h-11 px-5 flex items-center gap-3 border-b border-slate-200/60 transition-colors hover:bg-slate-50/80 cursor-pointer"
        >
            <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: category.color }} />
            <div className="min-w-0 flex-1" onClick={(e) => renaming && e.stopPropagation()}>
                {renaming ? (
                    <form
                        onSubmit={(e) => {
                            e.preventDefault();
                            void submitRename();
                        }}
                        className="flex items-center gap-1.5"
                    >
                        <TextInput value={title} onChange={setTitle} autoFocus className="w-64" />
                        <button type="submit" aria-label="Save" className="size-7 rounded-md text-emerald-700 hover:bg-emerald-50 inline-flex items-center justify-center">
                            <CheckIcon className="w-3.5 h-3.5" />
                        </button>
                        <button
                            type="button"
                            aria-label="Cancel"
                            onClick={() => {
                                setRenaming(false);
                                setTitle(category.title);
                            }}
                            className="size-7 rounded-md text-slate-500 hover:bg-slate-100 inline-flex items-center justify-center"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </form>
                ) : (
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{category.title}</span>
                )}
            </div>
            <span className="font-mono text-[12px] text-slate-900 tabular-nums w-20 text-right shrink-0">
                {count === undefined ? <span className="text-slate-300">…</span> : count.toLocaleString()}
            </span>
            <span className="text-[10.5px] uppercase tracking-[0.1em] text-slate-400 w-16 shrink-0 hidden sm:inline">contacts</span>
            <PopoverMenu align="end">
                <PopoverMenuTrigger asChild>
                    <button
                        type="button"
                        onClick={(e) => e.stopPropagation()}
                        aria-label="More"
                        className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
                    >
                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={200}>
                    <PopoverMenuItem onSelect={open}>View contacts</PopoverMenuItem>
                    <PopoverMenuItem onSelect={guarded(() => setRenaming(true))}>Rename</PopoverMenuItem>
                    <div className="px-2.5 py-1.5 flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                        {COLORS.map((color) => (
                            <button
                                key={color}
                                type="button"
                                aria-label={`Set color ${color}`}
                                onClick={guarded(() => void setColor(color))}
                                className={cn(
                                    "size-4 rounded-full border-2 transition-transform hover:scale-110",
                                    color === category.color ? "border-slate-900" : "border-transparent",
                                )}
                                style={{ backgroundColor: color }}
                            />
                        ))}
                    </div>
                    <PopoverMenuSeparator />
                    <PopoverMenuItem onSelect={guarded(askDelete)}>Delete</PopoverMenuItem>
                </PopoverMenuContent>
            </PopoverMenu>
        </div>
    );
}
