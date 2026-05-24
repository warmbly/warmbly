"use client";
import { motion } from "framer-motion";
import React from "react";

type Tab = 'Preset' | 'Custom'

interface TabsProps {
    tabs: Tab[];
    selectedTab: Tab;
    setSelectedTab: React.Dispatch<React.SetStateAction<Tab>>;
}

const Tabs = (props: TabsProps) => {
    const { tabs, selectedTab, setSelectedTab } = props;

    return (
        <div className="flex gap-2">
            {tabs.map((t) => (
                <div key={t} className="relative h-7 w-16 flex justify-center items-center">
                    {selectedTab === t && (
                        <motion.div
                            transition={{ type: "spring", duration: 0.3, bounce: 0.3 }}
                            layoutId="underline"
                            className="absolute top-0 left-0 h-full w-full border border-gray-300 bg-gray-100 rounded-lg"
                        />
                    )}
                    <button
                        onClick={() => setSelectedTab(t)}
                        className={`text-xs transition-colors z-1 ${selectedTab === t ? "text-gray-800" : "text-gray-400"}`}
                    >
                        {t}
                    </button>
                </div>
            ))}
        </div>
    )
}
export default Tabs;
