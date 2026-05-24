import { AnimatePresence, motion } from "framer-motion";
import React from "react";
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

  const calendarDays = generateCalendarDays();
  return (
    <AnimatePresence>
      {active && (
        <motion.div
          initial={{ opacity: 0, y: -10, scale: 0.95 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: -10, scale: 0.95 }}
          transition={{ type: "spring", duration: 0.2, bounce: 0.2 }}
          className="absolute top-full mt-2 z-50 bg-white border border-gray-200 rounded-xl shadow-xl overflow-hidden"
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
    </AnimatePresence>
  )
}
