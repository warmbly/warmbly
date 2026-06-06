// Label menu for a conversation. Lives in the ThreadView header and
// also opens via the `c` shortcut. Reuses the shared category registry
// (user.categories) and the same search-or-create pattern as the
// contacts CategoryPicker, but assigns at the thread level via
// PUT /unibox/thread/labels.

import React from "react";
import { CheckIcon, Loader2Icon, PlusIcon, TagIcon } from "lucide-react";
import toast from "react-hot-toast";

import {
  PopoverMenu,
  PopoverMenuContent,
  PopoverMenuLabel,
  PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useUserProfile } from "@/hooks/context/user";
import useCreateCategory from "@/lib/api/hooks/app/categories/useCreateCategory";
import useThreadLabels from "@/lib/api/hooks/app/unibox/useThreadLabels";
import useSetThreadLabels from "@/lib/api/hooks/app/unibox/useSetThreadLabels";

interface Props {
  threadId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}

export function ThreadLabelMenu({ threadId, open, onOpenChange }: Props) {
  const { user } = useUserProfile();
  const categories = React.useMemo(
    () => user.categories ?? [],
    [user.categories],
  );
  const labelsQ = useThreadLabels(threadId);
  const setLabels = useSetThreadLabels(threadId);
  const createCategory = useCreateCategory();
  const [query, setQuery] = React.useState("");

  const current = React.useMemo(() => labelsQ.data ?? [], [labelsQ.data]);
  const currentIds = React.useMemo(
    () => new Set(current.map((c) => c.id)),
    [current],
  );

  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return categories;
    return categories.filter((c) => c.title.toLowerCase().includes(q));
  }, [categories, query]);

  const queryMatchesExisting = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    return categories.some((c) => c.title.toLowerCase() === q);
  }, [categories, query]);

  const toggle = (id: string) => {
    const next = new Set(currentIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setLabels.mutate(Array.from(next));
  };

  const createAndAdd = async () => {
    const title = query.trim();
    if (!title) return;
    try {
      const c = await createCategory.mutateAsync(title);
      setLabels.mutate([...Array.from(currentIds), c.id]);
      setQuery("");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to create category",
      );
    }
  };

  return (
    <PopoverMenu
      align="end"
      side="bottom"
      open={open}
      onOpenChange={onOpenChange}
    >
      <PopoverMenuTrigger asChild>
        <button
          aria-label="Label this conversation (press c)"
          className="h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors text-[12px]"
        >
          {setLabels.isPending ? (
            <Loader2Icon className="w-3 h-3 animate-spin" />
          ) : (
            <TagIcon className="w-3.5 h-3.5" />
          )}
          Label
          {current.length > 0 && (
            <span className="font-mono tabular-nums text-[10px] text-slate-400">
              {current.length}
            </span>
          )}
        </button>
      </PopoverMenuTrigger>
      <PopoverMenuContent>
        <div className="w-[240px]">
          <PopoverMenuLabel>Label conversation</PopoverMenuLabel>
          <div className="px-1 pb-1">
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search or create…"
              autoFocus
              className="w-full h-7 px-2 rounded-md border border-slate-200 text-[12px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
            />
          </div>
          <div className="max-h-56 overflow-y-auto py-0.5">
            {categories.length === 0 && !query.trim() && (
              <div className="px-2.5 py-3 text-[11.5px] text-slate-400 text-center">
                No categories yet. Type to create one.
              </div>
            )}
            {filtered.map((c) => {
              const checked = currentIds.has(c.id);
              return (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => toggle(c.id)}
                  disabled={setLabels.isPending}
                  className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors disabled:opacity-60"
                >
                  <span
                    className={`size-3.5 rounded border flex items-center justify-center shrink-0 ${
                      checked
                        ? "border-slate-900 bg-slate-900"
                        : "border-slate-300 bg-white"
                    }`}
                  >
                    {checked && <CheckIcon className="w-2 h-2 text-white" />}
                  </span>
                  <span
                    className="size-2.5 rounded-full shrink-0"
                    style={{ backgroundColor: c.color }}
                  />
                  <span className="truncate">{c.title}</span>
                </button>
              );
            })}
            {query.trim() && !queryMatchesExisting && (
              <button
                type="button"
                onClick={createAndAdd}
                disabled={createCategory.isPending}
                className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-900 font-medium hover:bg-sky-50 border-t border-slate-100 transition-colors"
              >
                {createCategory.isPending ? (
                  <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />
                ) : (
                  <PlusIcon className="w-3 h-3 text-sky-600" />
                )}
                Create "{query.trim()}"
              </button>
            )}
          </div>
        </div>
      </PopoverMenuContent>
    </PopoverMenu>
  );
}
