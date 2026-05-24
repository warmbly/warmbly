import { useSortable } from "@dnd-kit/sortable";
import React from "react"
import { CSS } from "@dnd-kit/utilities";
import { AnimatePresence, motion } from "framer-motion";
import ModalColorInput from "./ModalColorInput";
import { Loading } from "@/components/loader";

const grabClass = "w-1 h-1 bg-slate-400"

export default function ModalBox({
    id,
    title,
    setTitle,
    color,
    setColor,
    onExpand,
    onSave,
    onDelete,
    deleteLoad,
    saveLoad,
    expanded,
    isChanged,
    disabled,
}: {
    id: string,
    title: string,
    setTitle: React.Dispatch<React.SetStateAction<string>>,
    color: string,
    setColor: React.Dispatch<React.SetStateAction<string>>,
    onExpand: () => void,
    onSave: () => Promise<void>,
    onDelete: () => Promise<void>,
    deleteLoad: boolean,
    saveLoad: boolean,
    expanded: boolean,
    isChanged: boolean,
    disabled: boolean,
}) {
    const ref = React.useRef<HTMLDivElement>(null);

    const { attributes, listeners, setNodeRef, transform, transition } =
        useSortable({ id, disabled: disabled });

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
    };

    return (
        <div
            ref={setNodeRef}
            style={style}
            className="bg-slate-200 rounded-lg shadow-md"
        >
            <div className="flex items-center h-12 px-4 gap-3 select-none">
                <div
                    {...attributes}
                    {...listeners}
                    className="grid grid-cols-2 gap-1 shrink-0"
                >
                    <div className='space-y-1'>
                        <div className={grabClass} />
                        <div className={grabClass} />
                        <div className={grabClass} />
                    </div>
                    <div className='space-y-1'>
                        <div className={grabClass} />
                        <div className={grabClass} />
                        <div className={grabClass} />
                    </div>
                </div>
                <div
                    className="flex-1 px-2 py-1 cursor-pointer flex justify-between gap-1 items-center"
                    onClick={onExpand}
                >
                    <span className='text-lg text-slate-600 truncate'>{title}</span>
                    <div
                        className="w-5 h-5 rounded-full shadow-sm shrink-0"
                        style={{ backgroundColor: `${color}` }}
                    />
                </div>
            </div>

            <AnimatePresence>
                {expanded && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ type: "spring", duration: .3 }}
                        style={{ overflow: "visible" }}
                        onAnimationStart={(def) => {
                            if ((def === "animate" || def === "exit") && ref.current) {
                                ref.current.style.overflow = "hidden";
                            }
                        }}
                        onAnimationComplete={() => {
                            if (ref.current) {
                                ref.current.style.overflow = "visible";
                            }
                        }}
                        className=" bg-slate-100 space-y-3"
                    >
                        <div className='p-3 overflow-visible'>
                            <div className='flex gap-3 items-center'>
                                <ModalColorInput color={color} setColor={setColor} />
                                <input
                                    type="text"
                                    value={title}
                                    onChange={(e) => setTitle(e.target.value)}
                                    placeholder='Folder Name'
                                    className="outline-none border-b border-slate-300 focus:border-slate-400 transition px-2 py-1 placeholder:text-slate-400 text-slate-600"
                                />
                            </div>
                            <div className='flex justify-between gap-4 flex-wrap items-center mt-4'>
                                <div className='flex gap-2 relative'>
                                    <button
                                        onClick={onSave}
                                        className={`px-3 py-2 w-33 flex justify-center items-center bg-blue-500 text-slate-50 hover:bg-blue-600 ripple rounded-lg cursor-pointer`}
                                    >
                                        {saveLoad ? <Loading className='h-5' /> : "Save Changes"}
                                    </button>
                                    <button
                                        onClick={() => {
                                            setTitle(title);
                                            setColor(color);
                                        }}
                                        className='px-3 py-2 bg-slate-300 hover:bg-slate-400 text-slate-700 ripple rounded-lg cursor-pointer'
                                    >
                                        Reset
                                    </button>
                                    <div className={`bg-slate-200 absolute inset-0 transition ${isChanged ? "opacity-60 visible" : "invisible opacity-0"}`} />
                                </div>
                                <button
                                    onClick={onDelete}
                                    className='px-3 py-2 w-20 transition bg-red-400 hover:bg-red-500 ripple text-slate-50 rounded-lg cursor-pointer'
                                >
                                    {deleteLoad ? <Loading className='h-5' /> : "Delete"}
                                </button>
                            </div>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    )
}
