import { AnimatePresence, motion } from "framer-motion"
import { AlertCircleIcon, LoaderIcon } from "lucide-react"

export default function LoadingScreen({
    errorTitle,
    errorMessage,
}: {
    errorTitle?: string,
    errorMessage?: string,
}) {
    return (
        <motion.div
            key="loader"
            className="fixed inset-0 z-50 bg-white flex items-center justify-center"
            initial={{ opacity: 1 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.3, ease: "easeInOut" }}
        >
            {!(errorTitle && errorMessage) && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                >
                    <LoaderIcon className="w-4 h-4 text-zinc-400 animate-spin" />
                </motion.div>
            )}

            <AnimatePresence mode="wait">
                {(errorTitle || errorMessage) && (
                    <motion.div
                        key="error"
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 8 }}
                        transition={{ duration: 0.3 }}
                        className="flex flex-col items-center text-center px-4 max-w-md"
                    >
                        <div className="w-10 h-10 rounded-xl bg-red-50 flex items-center justify-center mb-3">
                            <AlertCircleIcon className="w-4 h-4 text-red-600" />
                        </div>
                        <h1 className="text-sm font-semibold text-zinc-900 mb-1">
                            {errorTitle}
                        </h1>
                        <p className="text-[13px] text-zinc-500 leading-relaxed">
                            {errorMessage}
                        </p>
                    </motion.div>
                )}
            </AnimatePresence>
        </motion.div>
    )
}
