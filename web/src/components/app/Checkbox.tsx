import { RiCheckLine } from "@remixicon/react";
import { motion } from "framer-motion";
import { twColors } from "tailwindv4-colors";

const Checkbox = ({ checked }: { checked: boolean }) => (
  <motion.div
    style={{
      width: 20,
      height: 20,
      borderRadius: 4,
      border: '2px solid #9ca3af',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: checked ? '#3b82f6' : 'transparent',
      borderColor: checked ? '#3b82f6' : '#9ca3af',
      position: 'relative',
      overflow: 'hidden',
    }}
  >
    <motion.svg
      width="12"
      height="10"
      viewBox="0 0 12 10"
      initial={false}
      animate={{ pathLength: checked ? 1 : 0, opacity: checked ? 1 : 0 }}
      transition={{ duration: 0.25, ease: 'easeInOut' }}
    >
      <motion.path
        d="M1 5.5L4.5 9 11 1"
        stroke="#ffffff"
        strokeWidth="2"
        fill="none"
        strokeLinecap="round"
        strokeLinejoin="round"
        pathLength={1}
      />
    </motion.svg>

    <motion.div
      style={{
        position: 'absolute',
        inset: 0,
        borderRadius: 4,
        backgroundColor: '#3b82f6',
      }}
      initial={{ scale: 0 }}
      animate={{ scale: checked ? 1 : 0 }}
      transition={{ duration: 0.25, ease: 'easeInOut' }}
    />
    <RiCheckLine className={`w-3 absolute z-2 transition duration-500 ${checked ? "opacity-100":"opacity-0"}`} color={twColors.gray[50]}/>
  </motion.div>
);

export default Checkbox;