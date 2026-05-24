"use client";
import React, { useCallback, useEffect, useRef, useState } from "react";

type ClassValue =
  | ClassArray
  | string
  | number
  | bigint
  | null
  | boolean
  | undefined;
type ClassArray = ClassValue[];

interface hsl {
  h: number;
  s: number;
  l: number;
}

interface hex {
  hex: string;
}
type Color = hsl & hex;

function hslToHex({ h, s, l }: hsl) {
  s /= 100;
  l /= 100;

  const k = (n: number) => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = (n: number) =>
    l - a * Math.max(Math.min(k(n) - 3, 9 - k(n), 1), -1);
  const r = Math.round(255 * f(0));
  const g = Math.round(255 * f(8));
  const b = Math.round(255 * f(4));

  const toHex = (x: number) => {
    const hex = x.toString(16);
    return hex.length === 1 ? "0" + hex : hex;
  };

  return `${toHex(r)}${toHex(g)}${toHex(b)}`.toUpperCase();
}

function hexToHsl({ hex }: hex): hsl {
  // Ensure the hex string is formatted properly
  hex = hex.replace(/^#/, "");

  // Handle 3-digit hex
  if (hex.length === 3) {
    hex = hex
      .split("")
      .map((char) => char + char)
      .join("");
  }

  // Pad with zeros if incomplete
  while (hex.length < 6) {
    hex += "0";
  }

  // Convert hex to RGB
  let r = parseInt(hex.slice(0, 2), 16) || 0;
  let g = parseInt(hex.slice(2, 4), 16) || 0;
  let b = parseInt(hex.slice(4, 6), 16) || 0;

  // Then convert RGB to HSL
  r /= 255;
  g /= 255;
  b /= 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  let h = 0;
  let s: number;
  const l = (max + min) / 2;

  if (max === min) {
    h = s = 0; // achromatic
  } else {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r:
        h = (g - b) / d + (g < b ? 6 : 0);
        break;
      case g:
        h = (b - r) / d + 2;
        break;
      case b:
        h = (r - g) / d + 4;
        break;
    }
    h /= 6;
    h *= 360;
  }

  return { h: Math.round(h), s: Math.round(s * 100), l: Math.round(l * 100) };
}

const DraggableColorCanvas = ({
  h,
  s,
  l,
  handleChange,
}: hsl & {
  handleChange: (e: Partial<Color>) => void;
}) => {
  const [dragging, setDragging] = useState(false);
  const colorAreaRef = useRef<HTMLDivElement>(null);

  const calculateSaturationAndLightness = useCallback(
    (clientX: number, clientY: number) => {
      if (!colorAreaRef.current) return;
      const rect = colorAreaRef.current.getBoundingClientRect();
      const x = clientX - rect.left;
      const y = clientY - rect.top;
      const xClamped = Math.max(0, Math.min(x, rect.width));
      const yClamped = Math.max(0, Math.min(y, rect.height));
      const newSaturation = Math.round((xClamped / rect.width) * 100);
      const newLightness = 100 - Math.round((yClamped / rect.height) * 100);
      handleChange({ s: newSaturation, l: newLightness });
    },
    [handleChange],
  );

  const handleMouseMove = useCallback(
    (e: MouseEvent) => {
      e.preventDefault();
      calculateSaturationAndLightness(e.clientX, e.clientY);
    },
    [calculateSaturationAndLightness],
  );

  const handleMouseUp = useCallback(() => {
    setDragging(false);
  }, []);

  const handleMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragging(true);
    calculateSaturationAndLightness(e.clientX, e.clientY);
  };

  const handleTouchMove = useCallback(
    (e: TouchEvent) => {
      e.preventDefault();
      const touch = e.touches[0];
      if (touch) {
        calculateSaturationAndLightness(touch.clientX, touch.clientY);
      }
    },
    [calculateSaturationAndLightness],
  );

  const handleTouchEnd = useCallback(() => {
    setDragging(false);
  }, []);

  const handleTouchStart = (e: React.TouchEvent<HTMLDivElement>) => {
    e.preventDefault();
    const touch = e.touches[0];
    if (touch) {
      setDragging(true);
      calculateSaturationAndLightness(touch.clientX, touch.clientY);
    }
  };

  useEffect(() => {
    const touchOptions: AddEventListenerOptions = { passive: false };

    if (dragging) {
      window.addEventListener("mousemove", handleMouseMove);
      window.addEventListener("mouseup", handleMouseUp);
      window.addEventListener("mouseleave", handleMouseUp);
      window.addEventListener("touchmove", handleTouchMove, touchOptions);
      window.addEventListener("touchend", handleTouchEnd);
      window.addEventListener("touchcancel", handleTouchEnd);
    } else {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
      window.removeEventListener("mouseleave", handleMouseUp);
      window.removeEventListener("touchmove", handleTouchMove, touchOptions);
      window.removeEventListener("touchend", handleTouchEnd);
      window.removeEventListener("touchcancel", handleTouchEnd);
    }

    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
      window.removeEventListener("mouseleave", handleMouseUp);
      window.removeEventListener("touchmove", handleTouchMove, touchOptions);
      window.removeEventListener("touchend", handleTouchEnd);
      window.removeEventListener("touchcancel", handleTouchEnd);
    };
  }, [dragging, handleMouseMove, handleMouseUp, handleTouchMove, handleTouchEnd]);

  return (
    <div
      ref={colorAreaRef}
      className="h-22 w-full touch-auto overscroll-none rounded-xl border border-zinc-200"
      style={{
        background: `linear-gradient(to top, #000, transparent, #fff), linear-gradient(to left, hsl(${h}, 100%, 50%), #bbb)`,
        position: "relative",
        cursor: "crosshair",
      }}
      onMouseDown={handleMouseDown}
      onMouseUp={() => setDragging(false)}
      onTouchStart={handleTouchStart}
      onTouchEnd={() => setDragging(false)}
    >
      <div
        className="color-selector border-4 border-white ring-1 ring-zinc-200 dark:border-zinc-900 dark:ring-zinc-700"
        style={{
          position: "absolute",
          width: "20px",
          height: "20px",
          borderRadius: "50%",
          background: `hsl(${h}, ${s}%, ${l}%)`,
          transform: "translate(-50%, -50%)",
          left: `${s}%`,
          top: `${100 - l}%`,
          cursor: dragging ? "grabbing" : "grab",
        }}
      ></div>
    </div>
  );
};

function sanitizeHex(val: string) {
  const sanitized = val.replace(/[^a-zA-Z0-9]/g, "").toUpperCase();
  return sanitized;
}
const ColorPicker = ({ defColor, setColorX }: { defColor: string, setColorX: (color: string) => void }) => {
  // Initialize from controlled prop or a default
  const [color, setColor] = useState<Color>(() => {
    const hex = sanitizeHex(defColor);
    const hsl = hexToHsl({ hex: hex });
    return { ...hsl, hex: sanitizeHex(hex) };
  });

  React.useEffect(() => {
    setColorX("#" + color.hex)
  }, [color])

  return (
    <>
      <style
        id="slider-thumb-style"
        dangerouslySetInnerHTML={{
          // For the input range thumb styles. Some things are just easier to add to an external stylesheet.
          // don't actually put this in production.
          // Just putting this here for the sake of a single file in this example
          __html: `
              input[type='range']::-webkit-slider-thumb {
                -webkit-appearance: none;
                appearance: none;
                width: 18px; 
                height: 18px;
                background: transparent;
                border: 4px solid #FFFFFF;
                box-shadow: 0 0 0 1px #e4e4e7; 
                cursor: pointer;
                border-radius: 50%;
              }
              input[type='range']::-moz-range-thumb {
                width: 18px;
                height: 18px;
                cursor: pointer;
                border-radius: 50%;
                background: transparent;
                border: 4px solid #FFFFFF;
                box-shadow: 0 0 0 1px #e4e4e7;
              }
              input[type='range']::-ms-thumb {
                width: 18px;
                height: 18px;
                background: transparent;
                cursor: pointer;
                border-radius: 50%;
                border: 4px solid #FFFFFF;
                box-shadow: 0 0 0 1px #e4e4e7;
              }
    
              .dark input[type='range']::-webkit-slider-thumb {
                border: 4px solid rgb(24 24 27);
                box-shadow: 0 0 0 1px #3f3f46; 
              }
              .dark input[type='range']::-moz-range-thumb {
                border: 4px solid rgb(24 24 27);
                box-shadow: 0 0 0 1px #3f3f46; 
              }
              .dark input[type='range']::-ms-thumb {
                border: 4px solid rgb(24 24 27);
                box-shadow: 0 0 0 1px #3f3f46; 
              }
              `,
        }}
      />
      <div
        style={
          {
            "--thumb-border-color": "#000000",
            "--thumb-ring-color": "#666666",
          } as React.CSSProperties
        }
        className="z-30 flex w-full select-none flex-col items-center gap-3 overscroll-none rounded-2xl my-2"
      >
        <DraggableColorCanvas
          {...color}
          handleChange={(parital) => {
            setColor((prev) => {
              const value = { ...prev, ...parital };
              const hex_formatted = hslToHex({
                h: value.h,
                s: value.s,
                l: value.l,
              });
              return { ...value, hex: hex_formatted };
            });
          }}
        />
        <input
          type="range"
          min="0"
          max="360"
          value={color.h}
          className="dark:border-zinc-7000 h-3 w-full cursor-pointer appearance-none rounded-full border border-zinc-200 bg-white text-white placeholder:text-white"
          style={{
            background: `linear-gradient(to right, 
                    hsl(0, 100%, 50%), 
                    hsl(60, 100%, 50%), 
                    hsl(120, 100%, 50%), 
                    hsl(180, 100%, 50%), 
                    hsl(240, 100%, 50%), 
                    hsl(300, 100%, 50%), 
                    hsl(360, 100%, 50%))`,
          }}
          onChange={(e) => {
            const hue = e.target.valueAsNumber;
            setColor((prev) => {
              const { ...rest } = { ...prev, h: hue };
              const hex_formatted = hslToHex({ ...rest });
              return { ...rest, hex: hex_formatted };
            });
          }}
        />
      </div>
    </>
  );
};

export default ColorPicker;
