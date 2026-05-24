import { AnimatePresence, motion } from "framer-motion";

export default function SelectMenu({show, children, reverse}:{show: boolean, children: React.ReactNode, reverse?: boolean}){
    return (
        <AnimatePresence>
            {show && (
                <motion.div
                 transition={{type: "spring", duration: 0.3, bounce: 0.3}}
                 initial={{opacity: 0, scale: 0.6}}
                 animate={{opacity: 1, scale: 1}}
                 exit={{opacity: 0, scale: 0.6}}
                 className={`bg-white z-10 p-1.5 space-y-1 select-none shadow-sm absolute ${reverse ? "bottom-[calc(100%+6px)]":"top-[calc(100%+6px)]"} w-full max-h-60 overflow-y-scroll rounded-lg border border-slate-200`}
                 >
                    {children}
                </motion.div>
            )}
        </AnimatePresence>
    )
}