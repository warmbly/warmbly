"use client";

import { AnimatePresence, motion } from "framer-motion";

const ColorBox = ({children, show, top, right}:{children: React.ReactNode, show: boolean, top?: boolean, right?: boolean}) => {
    return (
        <AnimatePresence>
            {show && (
                <motion.div
                    className={`absolute ${right ? "right-0":"left-0"} ${top ? "bottom-full mb-2":"top-full mt-2"}`}
                    transition={{type: "spring", duration: 0.3, bounce: 0.3}}
                    initial={{opacity: 0, scale: 0.6}}
                    animate={{opacity: 1, scale: 1}}
                    exit={{opacity: 0, scale: 0.6}}
                >
                    <div className="rounded-xl w-57 h-52 p-2 bg-white border border-gray-200 shadow-md">
                        {children}
                    </div>
                </motion.div>
            )}
        </AnimatePresence>
    )
}

export default ColorBox;