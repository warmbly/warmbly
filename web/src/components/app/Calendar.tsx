import { AnimatePresence, motion } from "framer-motion";
import React from "react";
import { createPortal } from "react-dom";
import { format, addMonths, subMonths, startOfMonth, endOfMonth, startOfWeek, endOfWeek, eachDayOfInterval, isSameMonth, isSameDay, isToday } from 'date-fns';
import { RiArrowLeftSLine, RiArrowRightSLine } from "@remixicon/react";

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

  const handleDateSelect = (date: Date) => {
    onSubmit(date);
    close();
  };

  const generateCalendarDays = () => {
    const monthStart = startOfMonth(currentMonth);
    const monthEnd = endOfMonth(currentMonth);

    const calendarStart = startOfWeek(monthStart, { weekStartsOn: 1 });
    const calendarEnd = endOfWeek(monthEnd, { weekStartsOn: 1 });

    const days = eachDayOfInterval({ start: calendarStart, end: calendarEnd });

    return days;
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
      const cw = panel?.offsetWidth ?? 320; // w-80
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
                initial={{ opacity: 0, y: -10, scale: 0.95 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, y: -10, scale: 0.95 }}
                transition={{ type: "spring", duration: 0.2, bounce: 0.2 }}
                style={{
                  position: "fixed",
                  top: pos?.top ?? -9999,
                  left: pos?.left ?? -9999,
                  visibility: pos ? "visible" : "hidden",
                  zIndex: 100,
                }}
                className="bg-white border border-gray-200 rounded-xl shadow-xl overflow-hidden"
              >
                <div className="w-80">
                  <div className="flex items-center justify-between p-4 border-b border-gray-100">
                    <div
                      onClick={() => setCurrentMonth(subMonths(currentMonth, 1))}
                      className="p-1 ripple cursor-pointer hover:bg-gray-100 rounded-lg transition-colors"
                    >
                      <RiArrowLeftSLine className="w-5 h-5 text-gray-600" />
                    </div>

                    <span className="font-semibold text-gray-900">
                      {format(currentMonth, 'yyyy. MMMM')}
                    </span>

                    <div
                      onClick={() => setCurrentMonth(addMonths(currentMonth, 1))}
                      className="p-1 ripple cursor-pointer hover:bg-gray-100 rounded-lg transition-colors"
                    >
                      <RiArrowRightSLine className="w-5 h-5 text-gray-600" />
                    </div>
                  </div>

                  <div className="p-4">
                    <div className="grid grid-cols-7 gap-1 mb-2">
                      {['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'].map(day => (
                        <div key={day} className="text-center text-xs font-medium text-gray-500 py-2">
                          {day}
                        </div>
                      ))}
                    </div>

                    <div className="grid grid-cols-7 gap-1">
                      {calendarDays.map((day) => {
                        const isSelected = date && isSameDay(day, date);
                        const isCurrentDay = isToday(day);
                        const isCurrentMonth = isSameMonth(day, currentMonth);

                        return (
                          <div
                            key={day.toString()}
                            onClick={() => {
                              handleDateSelect(day)
                              if (!isCurrentMonth) {
                                setCurrentMonth(day)
                              }
                            }}
                            className={`
                                relative cursor-pointer ripple w-10 h-10 flex items-center justify-center text-sm rounded-lg transition-[background]
                                ${isSelected
                                ? 'bg-blue-500 text-white shadow-md'
                                : isCurrentDay
                                  ? 'bg-blue-50 text-blue-600 font-semibold'
                                  : isCurrentMonth
                                    ? 'text-gray-700 hover:bg-gray-100'
                                    : 'text-gray-200 hover:bg-gray-50'
                              }
                              `}
                          >
                            {format(day, 'd')}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                </div>
              </motion.div>
            )}
          </AnimatePresence>,
          document.body,
        )}
    </>
  )
}
