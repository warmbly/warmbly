"use client";

import ColorPicker from "./ColorPicker";

const CustomView = ({color, setColor}: {color: string, setColor: (color: string) => void}) => {
    return <ColorPicker defColor={color} setColorX={setColor}/>
}

export default CustomView;