"use client";
import {AnimatePresence, motion} from "framer-motion";
import React from "react";
import Tabs from "./Tabs";
import PresetView from "./PresetView";
import CustomView from "./CustomView";
import Hex from "./Hex";
import SaveButton from "./SaveButton";

type Tab = 'Preset' | 'Custom'

const tabs: Tab[] = ["Preset", "Custom"]

const ColorPanel = ({color, submitColor}:{color: string, submitColor: (color: string) => void}) => {
    const [selectedTab, setSelectedTab] = React.useState<Tab>(tabs[0]);
    const [colorx, setColor] = React.useState<string>(color);

    return (
        <>
            <Tabs
                tabs={tabs}
                selectedTab={selectedTab}
                setSelectedTab={setSelectedTab}
            />
            <AnimatePresence mode="wait">
                {selectedTab === "Preset" && (
                    <motion.div 
                        key={"preset"}
                        initial={{x:10, opacity:0}}
                        animate={{x:0, opacity: 1}}
                        exit={{x: -10, opacity: 0}}
                        transition={{duration: 0.2, type: "spring", bounce: 0.3}}
                    >
                        <PresetView color={colorx} setColor={setColor}/>
                    </motion.div>
                )}
                {selectedTab === "Custom" && (
                    <motion.div 
                        key={"custom"}
                        initial={{x:10, opacity:0}}
                        animate={{x:0, opacity: 1}}
                        exit={{x: -10, opacity: 0}}
                        transition={{duration: 0.2, type: "spring", bounce: 0.3}}
                    >
                        <CustomView color={colorx} setColor={setColor}/>
                    </motion.div>
                )}
            </AnimatePresence>
            <div className="flex justify-between">
                <Hex color={colorx}/>
                <SaveButton
                    color={colorx}
                    submit={submitColor}
                />
            </div>
        </>
    )
}
export default ColorPanel;