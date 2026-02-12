import { Toaster as HotToaster, ToastBar } from "react-hot-toast";
import { motion } from "motion/react";

export function Toaster() {
    return (
        <HotToaster position="top-center" toastOptions={{ duration: 4000 }}>
            {(t) => (
                <motion.div
                    initial={{ opacity: 0, y: -8, scale: 0.96 }}
                    animate={{
                        opacity: t.visible ? 1 : 0,
                        y: t.visible ? 0 : -8,
                        scale: t.visible ? 1 : 0.96,
                    }}
                    transition={{ duration: 0.25, ease: "easeOut" }}
                >
                    <ToastBar toast={t} style={{ animation: "none" }} />
                </motion.div>
            )}
        </HotToaster>
    );
}
