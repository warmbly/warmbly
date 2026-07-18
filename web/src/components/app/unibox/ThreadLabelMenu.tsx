// Label menu for a conversation. Lives in the ThreadView header and also
// opens via the `c` shortcut. Shares the category registry (user.categories)
// and the CategoryPicker visual language: assigned labels render as colored
// chips right in the trigger, the panel has a search-or-create header, an
// assigned-chips row, and color-dotted checkbox rows. Assigns at the thread
// level via PUT /unibox/thread/labels.

import React from "react";
import { CheckIcon, Loader2Icon, PlusIcon, TagIcon } from "lucide-react";
import toast from "react-hot-toast";

import {
  PopoverMenu,
  PopoverMenuContent,
  PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { CategoryChip } from "@/components/app/contacts/CategoryPicker";
import { useUserProfile } from "@/hooks/context/user";
import useCreateCategory from "@/lib/api/hooks/app/categories/useCreateCategory";
import useThreadLabels from "@/lib/api/hooks/app/unibox/useThreadLabels";
import useSetThreadLabels from "@/lib/api/hooks/app/unibox/useSetThreadLabels";

interface Props {
  threadId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}

// How many assigned chips render inline in the trigger before "+N".
const TRIGGER_CHIPS = 2;

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

  React.useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

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

  const inline = current.slice(0, TRIGGER_CHIPS);
  const overflow = current.length - inline.length;

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
          title="Label this conversation (c)"
          className={`h-7 px-1.5 rounded-md inline-flex items-center gap-1.5 transition-colors text-[12px] ${
            open
              ? "bg-slate-100 text-slate-900"
              : "text-slate-500 hover:text-slate-900 hover:bg-slate-100"
          }`}
        >
          {setLabels.isPending ? (
            <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
          ) : (
            <TagIcon className="w-3.5 h-3.5" />
          )}
          {current.length === 0 ? (
            <span className="hidden sm:inline">Label</span>
          ) : (
            <span className="hidden sm:inline-flex items-center gap-1">
              {inline.map((c) => (
                <CategoryChip key={c.id} category={c} compact />
              ))}
              {overflow > 0 && (
                <span className="h-4 px-1 rounded bg-slate-100 text-[10px] font-medium text-slate-500 inline-flex items-center">
                  +{overflow}
                </span>
              )}
            </span>
          )}
        </button>
      </PopoverMenuTrigger>
      <PopoverMenuContent className="p-0">
        <div className="w-[260px]">
          {/* Search-or-create header. */}
          <div className="px-2.5 py-2 border-b border-slate-200 flex items-center gap-1.5">
            <TagIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && query.trim() && !queryMatchesExisting) {
                  e.preventDefault();
                  void createAndAdd();
                }
              }}
              placeholder="Label conversation…"
              autoFocus
              className="flex-1 min-w-0 h-5 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
            />
            {setLabels.isPending && (
              <Loader2Icon className="w-3 h-3 animate-spin text-slate-300 shrink-0" />
            )}
          </div>

          {/* Assigned chips, removable in place. */}
          {current.length > 0 && (
            <div className="px-2.5 py-2 border-b border-slate-100 flex flex-wrap gap-1">
              {current.map((c) => (
                <CategoryChip
                  key={c.id}
                  category={c}
                  onRemove={() => toggle(c.id)}
                />
              ))}
            </div>
          )}

          <div className="max-h-56 overflow-y-auto py-1">
            {categories.length === 0 && !query.trim() && (
              <div className="px-3 py-4 text-center">
                <div className="text-[12px] text-slate-500">No labels yet</div>
                <div className="text-[11px] text-slate-400 mt-0.5">
                  Type a name above to create your first one.
                </div>
              </div>
            )}
            {filtered.length === 0 && categories.length > 0 && queryMatchesExisting && (
              <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                No matches.
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
                  className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-50 transition-colors disabled:opacity-60"
                >
                  <span
                    className={`size-3.5 rounded border flex items-center justify-center transition-colors shrink-0 ${
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
                  {checked && (
                    <span className="ml-auto text-[10px] text-slate-300">assigned</span>
                  )}
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

          <div className="px-2.5 h-7 border-t border-slate-100 flex items-center justify-between text-[10px] text-slate-400">
            <span>Labels are shared with contact categories</span>
            <kbd className="h-4 px-1 rounded border border-slate-200 bg-slate-50 font-mono inline-flex items-center">
              c
            </kbd>
          </div>
        </div>
      </PopoverMenuContent>
    </PopoverMenu>
  );
}
