import { AnimatePresence, motion } from "framer-motion";
import React from "react";
import { createPortal } from "react-dom";
import {
  format,
  addMonths,
  subMonths,
  startOfMonth,
  endOfMonth,
  startOfWeek,
  endOfWeek,
  eachDayOfInterval,
  isSameMonth,
  isSameDay,
  isToday,
} from "date-fns";
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";

export default function Calendar({
  date,
  active,
  close,
  onSubmit,
}: {
  date: null | Date,
  active: boolean,
  close: () => void;
  onSubmit: (d: Date | null) => void,
}) {
  const [currentMonth, setCurrentMonth] = React.useState(date || new Date());

  // Zero-size anchor rendered at the calendar's natural DOM position (inside
  // the caller's `relative` trigger wrapper). We measure its parent — the
  // trigger box — to position the portaled panel, instead of relying on
  // `absolute top-full`, which clips inside overflow-hidden/scroll ancestors
  // (modals, scroll areas, cards) and never flips up when there's no room.
  const anchorRef = React.useRef<HTMLSpanElement>(null);
  const panelRef = React.useRef<HTMLDivElement>(null);
  const [pos, setPos] = React.useState<{ top: number; left: number } | null>(null);

  const handleDateSelect = (d: Date) => {
    onSubmit(d);
    close();
  };

  const generateCalendarDays = () => {
    const monthStart = startOfMonth(currentMonth);
    const monthEnd = endOfMonth(currentMonth);

    const calendarStart = startOfWeek(monthStart, { weekStartsOn: 1 });
    const calendarEnd = endOfWeek(monthEnd, { weekStartsOn: 1 });

    return eachDayOfInterval({ start: calendarStart, end: calendarEnd });
  };

  // Position the portaled panel fixed at the trigger rect, flipping above the
  // trigger when there isn't room below in the viewport.
  React.useLayoutEffect(() => {
    if (!active) {
      setPos(null);
      return;
    }
    const compute = () => {
      const trigger = anchorRef.current?.parentElement;
      const panel = panelRef.current;
      if (!trigger) return;
      const r = trigger.getBoundingClientRect();
      const sideOffset = 8;
      const ch = panel?.offsetHeight ?? 0;
      const cw = panel?.offsetWidth ?? 264;
      let top = r.bottom + sideOffset;
      if (ch && top + ch > window.innerHeight - 8) {
        const above = r.top - ch - sideOffset;
        if (above >= 8) top = above;
      }
      let left = r.left;
      if (cw && left + cw > window.innerWidth - 8) left = window.innerWidth - 8 - cw;
      if (left < 8) left = 8;
      setPos({ top, left });
    };
    compute();
    window.addEventListener("resize", compute);
    window.addEventListener("scroll", compute, true);
    return () => {
      window.removeEventListener("resize", compute);
      window.removeEventListener("scroll", compute, true);
    };
  }, [active, currentMonth]);

  // The panel is portaled to <body>, outside the caller's wrapper ref. The
  // callers dismiss on a document "mousedown" that checks ref.contains(target),
  // so a mousedown inside the (portaled) panel would otherwise close the
  // calendar on month-nav. Stop mousedown/touchstart at the panel via a NATIVE
  // listener (reliable regardless of React's event delegation) — "click" is not
  // stopped, so date selection and month navigation still fire.
  React.useEffect(() => {
    const el = panelRef.current;
    if (!active || !el) return;
    const stop = (e: Event) => e.stopPropagation();
    el.addEventListener("mousedown", stop);
    el.addEventListener("touchstart", stop);
    return () => {
      el.removeEventListener("mousedown", stop);
      el.removeEventListener("touchstart", stop);
    };
  }, [active, pos]);

  const calendarDays = generateCalendarDays();
  return (
    <>
      <span ref={anchorRef} className="hidden" aria-hidden="true" />
      {typeof document !== "undefined" &&
        createPortal(
          <AnimatePresence>
            {active && (
              <motion.div
                ref={panelRef}
                initial={{ opacity: 0, y: -6, scale: 0.97 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, y: -6, scale: 0.97 }}
                transition={{ type: "spring", duration: 0.18, bounce: 0.15 }}
                style={{
                  position: "fixed",
                  top: pos?.top ?? -9999,
                  left: pos?.left ?? -9999,
                  visibility: pos ? "visible" : "hidden",
                  zIndex: 100,
                }}
                data-floating="true"
                className="w-[264px] bg-white border border-slate-200 rounded-lg shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18),0_2px_8px_-4px_rgba(15,23,42,0.10)] overflow-hidden"
              >
                {/* month header */}
                <div className="flex items-center justify-between px-3 h-11 border-b border-slate-200/70">
                  <button
                    type="button"
                    aria-label="Previous month"
                    onClick={() => setCurrentMonth(subMonths(currentMonth, 1))}
                    className="size-7 rounded-md inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                  >
                    <ChevronLeftIcon className="w-4 h-4" />
                  </button>
                  <span className="text-[12.5px] font-medium text-slate-900 tabular-nums">
                    {format(currentMonth, "MMMM yyyy")}
                  </span>
                  <button
                    type="button"
                    aria-label="Next month"
                    onClick={() => setCurrentMonth(addMonths(currentMonth, 1))}
                    className="size-7 rounded-md inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                  >
                    <ChevronRightIcon className="w-4 h-4" />
                  </button>
                </div>

                <div className="p-3">
                  {/* weekday labels */}
                  <div className="grid grid-cols-7 gap-1 mb-1">
                    {["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"].map((day) => (
                      <div
                        key={day}
                        className="text-center text-[10px] font-medium uppercase tracking-[0.06em] text-slate-400 py-1"
                      >
                        {day}
                      </div>
                    ))}
                  </div>

                  {/* day grid */}
                  <div className="grid grid-cols-7 gap-1">
                    {calendarDays.map((day) => {
                      const isSelected = date && isSameDay(day, date);
                      const isCurrentDay = isToday(day);
                      const isCurrentMonth = isSameMonth(day, currentMonth);

                      return (
                        <button
                          key={day.toString()}
                          type="button"
                          onClick={() => {
                            handleDateSelect(day);
                            if (!isCurrentMonth) setCurrentMonth(day);
                          }}
                          className={`aspect-square w-full inline-flex items-center justify-center text-[12px] rounded-md transition-colors ${
                            isSelected
                              ? "bg-sky-600 text-white font-medium"
                              : isCurrentDay
                                ? "bg-sky-50 text-sky-700 font-medium hover:bg-sky-100"
                                : isCurrentMonth
                                  ? "text-slate-700 hover:bg-slate-100"
                                  : "text-slate-300 hover:bg-slate-50"
                          }`}
                        >
                          {format(day, "d")}
                        </button>
                      );
                    })}
                  </div>
                </div>

                {/* footer: clear + today */}
                <div className="flex items-center justify-between px-3 h-9 border-t border-slate-200/70">
                  <button
                    type="button"
                    onClick={() => {
                      onSubmit(null);
                      close();
                    }}
                    className="text-[11.5px] text-slate-500 hover:text-slate-900 transition-colors"
                  >
                    Clear
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setCurrentMonth(new Date());
                      handleDateSelect(new Date());
                    }}
                    className="text-[11.5px] font-medium text-sky-600 hover:text-sky-700 transition-colors"
                  >
                    Today
                  </button>
                </div>
              </motion.div>
            )}
          </AnimatePresence>,
          document.body,
        )}
    </>
  );
}
