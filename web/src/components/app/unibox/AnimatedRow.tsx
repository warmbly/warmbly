// The motion shell around one conversation row.
//
// Three layers: the shell folds the row's height, the content slides out in
// the direction of the action, and the underlay is what the slide uncovers.
// The underlay's colour and label are picked by CSS from the `data-exit`
// attribute markRowExit stamps on the shell (see lib/unibox/rowMotion).

import React from "react";
import { motion } from "framer-motion";
import { ArchiveIcon, InboxIcon, MoonIcon, TrashIcon } from "lucide-react";

import {
  rowContent,
  rowShell,
  rowUnderlay,
  type RowMotionCustom,
} from "@/lib/unibox/rowMotion";

const UNDERLAY =
  "hidden absolute inset-0 items-center gap-1.5 text-[11.5px] font-medium";

export function AnimatedRow({
  motionKey,
  custom,
  children,
}: {
  motionKey: string;
  custom: RowMotionCustom;
  children: React.ReactNode;
}) {
  return (
    <motion.div
      data-thread-id={motionKey}
      custom={custom}
      variants={rowShell}
      initial="enter"
      animate="shown"
      exit="exit"
      className="group/row relative overflow-hidden border-b border-slate-100"
    >
      <motion.div
        aria-hidden
        custom={custom}
        variants={rowUnderlay}
        className="absolute inset-0 pointer-events-none"
      >
        <span className={`${UNDERLAY} pl-5 bg-sky-50 text-sky-700 group-data-[exit=archive]/row:flex`}>
          <ArchiveIcon className="w-3.5 h-3.5" />
          Archived
        </span>
        <span className={`${UNDERLAY} justify-end pr-5 bg-rose-50 text-rose-600 group-data-[exit=trash]/row:flex`}>
          <TrashIcon className="w-3.5 h-3.5" />
          Moved to Trash
        </span>
        <span className={`${UNDERLAY} justify-end pr-5 bg-emerald-50 text-emerald-700 group-data-[exit=inbox]/row:flex`}>
          <InboxIcon className="w-3.5 h-3.5" />
          Moved to Inbox
        </span>
        <span className={`${UNDERLAY} pl-5 bg-amber-50 text-amber-700 group-data-[exit=snooze]/row:flex`}>
          <MoonIcon className="w-3.5 h-3.5" />
          Snoozed
        </span>
      </motion.div>
      <motion.div custom={custom} variants={rowContent} className="relative bg-white">
        {children}
      </motion.div>
    </motion.div>
  );
}
