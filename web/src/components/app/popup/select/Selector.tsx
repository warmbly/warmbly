import { RiArrowRightSLine } from "@remixicon/react";
import { AnimatePresence, motion } from "framer-motion";

export default function Selector({children, show, setShow, caret=false}:{children: React.ReactNode, show: boolean, setShow: (v: boolean) => void, caret?: boolean}){
    return <div className={`border select-none cursor-pointer transition shadow-sm ${show ? "border-slate-300":"border-slate-200 hover:border-slate-300"} rounded-md px-3 py-2 flex justify-between gap-2`} onClick={() => setShow(!show)}>
        <div>
            {children}
        </div>
        {caret && (
        <div>
            <AnimatePresence >
                <motion.div
                    animate={{ rotate: show ? -90 : 0 }}
                    initial={false}
                    transition={{type: "spring", duration: 0.3, bounce: 0.3}}
                >
                    <RiArrowRightSLine className={`w-5`}/>
                </motion.div>
            </AnimatePresence>
        </div>)}
    </div>
}