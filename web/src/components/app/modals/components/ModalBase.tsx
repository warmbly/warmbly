import type React from "react"
import { motion } from "framer-motion";
import { RiCloseLine } from "@remixicon/react";

export default function ModalBase({
    children,
    close,
}: {
    children: React.ReactNode,
    close: () => void,
}) {
    return (

        <motion.div
            className='bg-black/45 fixed z-100 inset-0 p-3'
            transition={{ type: "spring", duration: 0.3, bounce: 0.3 }}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
        >
            <motion.div
                className='bg-white h-full w-full relative rounded-3xl py-10 px-10 sm:py-15 sm:px-20'
                transition={{ type: "spring", duration: 0.3, bounce: 0.3 }}
                initial={{ scale: .6 }}
                animate={{ scale: 1 }}
                exit={{ scale: .6 }}
            >
                <div onClick={close} className='absolute top-7 right-7 text-slate-500 hover:opacity-70 transition cursor-pointer'>
                    <RiCloseLine className='w-5' />
                </div>
                {children}
            </motion.div>
        </motion.div>
    )
}
