// Appearance settings — theme, glassmorphism, and background controls.
//
// All values are persisted client-side via the appearance slice in
// the Zustand store; nothing is sent to the backend.

import React from "react";
import { CheckIcon, ImageIcon, TrashIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import {
    Section,
    SectionShell,
    ToggleRow,
} from "../_components/SectionShell";
import { DitherSlider } from "@/components/ui/dither";
import { useAppStore } from "@/stores";

const PRESET_OPTIONS = [
    { value: "default", label: "Default" },
    { value: "gradient-1", label: "Sky fade" },
    { value: "gradient-4", label: "Blue fade" },
    { value: "gradient-5", label: "Monochrome" },
] as const;

const PRESET_BACKGROUNDS: Record<string, string> = {
    "gradient-1": "linear-gradient(170deg, #f8fafc 0%, #e0f2fe 18%, #fef3c7 48%, #fde68a 78%, #fefce8 100%)",
    "gradient-4": "linear-gradient(170deg, #f8fafc 0%, #e0f2fe 18%, #bfdbfe 48%, #93c5fd 78%, #dbeafe 100%)",
    "gradient-5": "linear-gradient(170deg, #f8fafc 0%, #f1f5f9 25%, #e2e8f0 55%, #cbd5e1 100%)",
};

const BUILTIN_BACKGROUNDS = [
    { value: "bg-2", label: "BG1", src: "/backgrounds/bg-2.webp" },
    { value: "bg-3", label: "BG2", src: "/backgrounds/bg-3.webp" },
] as const;

const MAX_DATA_URL_BYTES = 200 * 1024

export default function AppearanceSettingsPage() {
    return <AppearanceSettings />;
}

function AppearanceSettings() {
    const resolvedTheme = useAppStore((state) => state.resolvedTheme);
    const glassmorphismEnabled = useAppStore((state) => state.glassmorphismEnabled);
    const glassOpacity = useAppStore((state) => state.glassOpacity);
    const glassBlur = useAppStore((state) => state.glassBlur);
    const backgroundPreset = useAppStore((state) => state.backgroundPreset);
    const backgroundImage = useAppStore((state) => state.backgroundImage);
    const backgroundBlur = useAppStore((state) => state.backgroundBlur);
    const backgroundOpacity = useAppStore((state) => state.backgroundOpacity);
    const setGlassmorphismEnabled = useAppStore((state) => state.setGlassmorphismEnabled);
    const setGlassOpacity = useAppStore((state) => state.setGlassOpacity);
    const setGlassBlur = useAppStore((state) => state.setGlassBlur);
    const setBackgroundPreset = useAppStore((state) => state.setBackgroundPreset);
    const setBackgroundImage = useAppStore((state) => state.setBackgroundImage);
    const setBackgroundBlur = useAppStore((state) => state.setBackgroundBlur);
    const setBackgroundOpacity = useAppStore((state) => state.setBackgroundOpacity);

    const isDark = resolvedTheme === "dark";
    const hasCustomImage = Boolean(backgroundImage);

    return (
        <SectionShell
            title="Appearance"
            description="Customize how Warmbly looks for you. Changes apply instantly."
        >
            <div className="px-4 py-4 md:px-8 md:py-5">
                <AppearancePreview
                    isDark={isDark}
                    glassmorphismEnabled={glassmorphismEnabled}
                    glassOpacity={glassOpacity}
                    glassBlur={glassBlur}
                    backgroundPreset={backgroundPreset}
                    backgroundImage={backgroundImage}
                    backgroundBlur={backgroundBlur}
                    backgroundOpacity={backgroundOpacity}
                />
            </div>

            <Section
                eyebrow="Glassmorphism"
                description="Add a subtle frosted-glass effect to panels, cards, and dialogs. Off by default."
            >
                <ToggleRow
                    label="Enable glassmorphism"
                    description="Applies a translucent, blurred surface to the sidebar, navigation, cards, and dialogs."
                    checked={glassmorphismEnabled}
                    onChange={setGlassmorphismEnabled}
                />
                {glassmorphismEnabled && (
                    <div className="mt-4 space-y-5">
                        <SliderRow
                            label="Glass opacity"
                            description="Surface transparency. Higher = more transparent."
                            value={glassOpacity}
                            onChange={setGlassOpacity}
                            min={60}
                            max={100}
                            unit="%"
                        />
                        <SliderRow
                            label="Glass blur"
                            description="Blur strength in pixels."
                            value={glassBlur}
                            onChange={setGlassBlur}
                            min={0}
                            max={40}
                            unit="px"
                        />
                    </div>
                )}
            </Section>

            <Section
                eyebrow="Background"
                description="Set a background image behind the application. Stored locally in your browser."
            >
                <div className="space-y-5">
                    <div>
                        <div className="text-[12.5px] font-medium text-slate-900 mb-2">Preset</div>
                        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                            {PRESET_OPTIONS.map((preset) => {
                                const active = backgroundPreset === preset.value && !hasCustomImage;
                                return (
                                    <button
                                        type="button"
                                        key={preset.value}
                                        onClick={() => {
                                            setBackgroundPreset(preset.value as typeof backgroundPreset);
                                            if (preset.value !== "default") {
                                                setBackgroundImage("");
                                            }
                                        }}
                                        className={cn(
                                            "relative rounded-lg border-2 p-1.5 text-left transition-all duration-200",
                                            active
                                                ? "border-sky-500 shadow-sm"
                                                : "border-slate-200 hover:border-slate-300"
                                        )}
                                    >
                                        <div
                                            className="h-10 rounded-md mb-1.5"
                                            style={{
                                                background:
                                                    preset.value === "default"
                                                        ? "var(--secondary)"
                                                        : PRESET_BACKGROUNDS[preset.value],
                                            }}
                                        />
                                        <div className="text-[11px] font-medium text-slate-700 px-0.5">
                                            {preset.label}
                                        </div>
                                        {active && (
                                            <div className="absolute top-1.5 right-1.5 size-4 rounded-full bg-sky-500 text-white flex items-center justify-center">
                                                <CheckIcon className="w-2.5 h-2.5" />
                                            </div>
                                        )}
                                    </button>
                                );
                            })}
                        </div>
                    </div>

                    <div>
                        <div className="text-[12.5px] font-medium text-slate-900 mb-2">Background images</div>
                        <div className="grid grid-cols-4 gap-2 mb-3">
                            {BUILTIN_BACKGROUNDS.map((bg) => {
                                const isActive = backgroundImage === bg.src
                                return (
                                    <button
                                        type="button"
                                        key={bg.value}
                                        onClick={() => {
                                            setBackgroundImage(bg.src)
                                            setBackgroundPreset("default")
                                        }}
                                        className={cn(
                                            "relative rounded-lg border-2 overflow-hidden transition-all duration-200",
                                            isActive
                                                ? "border-sky-500 shadow-sm"
                                                : "border-slate-200 hover:border-slate-300"
                                        )}
                                    >
                                        <div
                                            className="h-16 w-full"
                                            style={{
                                                backgroundImage: `url(${bg.src})`,
                                                backgroundSize: "cover",
                                                backgroundPosition: "center",
                                            }}
                                        />
                                        <div className="text-[10px] font-medium text-slate-700 px-1 py-0.5 bg-white/80">
                                            {bg.label}
                                        </div>
                                        {isActive && (
                                            <div className="absolute top-1 right-1 size-3.5 rounded-full bg-sky-500 text-white flex items-center justify-center">
                                                <CheckIcon className="w-2 h-2" />
                                            </div>
                                        )}
                                    </button>
                                )
                            })}
                        </div>
                        <div className="text-[11px] text-slate-500 mb-2">Or upload your own</div>
                        <BackgroundImageUploader
                            current={backgroundImage}
                            onSelect={(url) => {
                                setBackgroundImage(url)
                                if (url) setBackgroundPreset("default")
                            }}
                            onClear={() => {
                                setBackgroundImage("")
                                setBackgroundPreset("default")
                            }}
                        />
                    </div>

                    <SliderRow
                        label="Background blur"
                        description="Blur applied to the background image."
                        value={backgroundBlur}
                        onChange={setBackgroundBlur}
                        min={0}
                        max={40}
                        unit="px"
                    />
                    <SliderRow
                        label="Background opacity"
                        description="Background visibility."
                        value={backgroundOpacity}
                        onChange={setBackgroundOpacity}
                        min={0}
                        max={100}
                        unit="%"
                    />
                </div>
            </Section>
        </SectionShell>
    );
}

function SliderRow({
    label,
    description,
    value,
    onChange,
    min,
    max,
    unit,
}: {
    label: string;
    description?: string;
    value: number;
    onChange: (v: number) => void;
    min: number;
    max: number;
    unit: string;
}) {
    return (
        <div className="space-y-2.5">
            <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                    <div className="text-[12.5px] font-medium text-slate-900">{label}</div>
                    {description && (
                        <div className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">{description}</div>
                    )}
                </div>
                <span className="text-[12px] text-slate-600 font-mono tabular-nums min-w-[3.5ch] text-right shrink-0">
                    {value}
                    {unit}
                </span>
            </div>
            <DitherSlider
                value={value}
                min={min}
                max={max}
                step={1}
                onChange={onChange}
                tone="sky"
            />
        </div>
    );
}

function AppearancePreview({
    isDark,
    glassmorphismEnabled,
    glassOpacity,
    glassBlur,
    backgroundPreset,
    backgroundImage,
    backgroundBlur,
    backgroundOpacity,
}: {
    isDark: boolean;
    glassmorphismEnabled: boolean;
    glassOpacity: number;
    glassBlur: number;
    backgroundPreset: string;
    backgroundImage: string;
    backgroundBlur: number;
    backgroundOpacity: number;
}) {
    const bgStyle: React.CSSProperties = {};
    if (backgroundImage) {
        bgStyle.backgroundImage = `url(${backgroundImage})`;
        bgStyle.backgroundSize = "cover";
        bgStyle.backgroundPosition = "center";
        bgStyle.backgroundRepeat = "no-repeat";
    } else if (backgroundPreset !== "default" && PRESET_BACKGROUNDS[backgroundPreset]) {
        bgStyle.background = PRESET_BACKGROUNDS[backgroundPreset];
    }

    const surface = glassmorphismEnabled
        ? isDark
            ? `rgba(30, 30, 40, ${glassOpacity / 100})`
            : `rgba(255, 255, 255, ${glassOpacity / 100})`
        : isDark
          ? "rgb(22, 22, 30)"
          : "rgb(255, 255, 255)";

    const border = glassmorphismEnabled
        ? isDark
            ? "rgba(255, 255, 255, 0.08)"
            : "rgba(0, 0, 0, 0.06)"
        : isDark
          ? "rgb(51, 51, 65)"
          : "rgb(226, 232, 240)";

    const shadow = glassmorphismEnabled
        ? "0 8px 32px -8px rgba(0, 0, 0, 0.12)"
        : "none";

    return (
        <div className="relative h-40 w-full overflow-hidden rounded-xl border border-slate-200 bg-slate-100">
            <div
                className="absolute inset-0 transition-all duration-500 ease-out"
                style={{
                    ...bgStyle,
                    opacity: backgroundImage || backgroundPreset !== "default" ? backgroundOpacity / 100 : 1,
                    filter: backgroundBlur ? `blur(${backgroundBlur}px)` : undefined,
                }}
            />

            <div
                className="relative z-10 flex h-full transition-all duration-300"
                style={{
                    background: surface,
                    backdropFilter: glassmorphismEnabled ? `blur(${glassBlur}px)` : undefined,
                    WebkitBackdropFilter: glassmorphismEnabled ? `blur(${glassBlur}px)` : undefined,
                    borderLeft: `1px solid ${border}`,
                    borderBottom: `1px solid ${border}`,
                    boxShadow: shadow,
                }}
            >
                <div
                    className="w-12 border-r flex flex-col items-center py-3 gap-2 transition-colors duration-300"
                    style={{ borderColor: border }}
                >
                    <div className={cn("size-4 rounded-md", isDark ? "bg-slate-700" : "bg-slate-200")} />
                    <div className={cn("size-4 rounded-md", isDark ? "bg-slate-800" : "bg-slate-50")} />
                    <div className={cn("size-4 rounded-md", isDark ? "bg-slate-800" : "bg-slate-50")} />
                </div>

                <div className="flex-1 p-4 flex flex-col gap-2.5">
                    <div
                        className={cn("h-2 rounded-full transition-colors duration-300", isDark ? "bg-slate-600" : "bg-slate-200")}
                        style={{ width: "35%" }}
                    />
                    <div
                        className={cn("h-2 rounded-full transition-colors duration-300", isDark ? "bg-slate-700" : "bg-slate-100")}
                        style={{ width: "65%" }}
                    />

                    <div
                        className={cn(
                            "mt-auto rounded-lg p-3 transition-all duration-300",
                            glassmorphismEnabled
                                ? isDark
                                    ? "bg-white/10 border"
                                    : "bg-white/80 border shadow-sm"
                                : isDark
                                  ? "bg-slate-800 border"
                                  : "bg-slate-50 border"
                        )}
                        style={{ borderColor: glassmorphismEnabled ? (isDark ? "rgba(255,255,255,0.1)" : "rgba(0,0,0,0.04)") : undefined }}
                    >
                        <div
                            className={cn("h-1.5 rounded-full mb-2 transition-colors duration-300", isDark ? "bg-slate-600" : "bg-slate-200")}
                            style={{ width: "50%" }}
                        />
                        <div
                            className={cn("h-1.5 rounded-full transition-colors duration-300", isDark ? "bg-slate-700" : "bg-slate-100")}
                            style={{ width: "75%" }}
                        />
                    </div>
                </div>
            </div>
        </div>
    );
}

function BackgroundImageUploader({
    current,
    onSelect,
    onClear,
}: {
    current: string;
    onSelect: (url: string) => void;
    onClear: () => void;
}) {
    const inputRef = React.useRef<HTMLInputElement>(null);
    const [preview, setPreview] = React.useState<string | null>(current || null);
    const [dragging, setDragging] = React.useState(false);
    const [error, setError] = React.useState<string | null>(null);
    const uploadGenerationRef = React.useRef(0);

    React.useEffect(() => {
        setPreview(current || null);
        setError(null);
    }, [current]);

    function readFileAsDataURL(file: File): Promise<string> {
        return new Promise((resolve, reject) => {
            const reader = new FileReader()
            reader.onload = () => resolve(reader.result as string)
            reader.onerror = () => reject(reader.error)
            reader.readAsDataURL(file)
        })
    }

    async function handleFile(file: File) {
        const generation = ++uploadGenerationRef.current
        setError(null)
        if (!file.type.startsWith("image/")) {
            setError("Please select an image file.")
            return
        }
        if (file.size > 10 * 1024 * 1024) {
            setError("Image must be smaller than 10 MB.")
            return
        }
        try {
            const dataUrl = await readFileAsDataURL(file)
            if (generation !== uploadGenerationRef.current) return
            if (dataUrl.length > MAX_DATA_URL_BYTES) {
                setError("Image is too large. Use a smaller file for browser storage.")
                return
            }
            if (generation !== uploadGenerationRef.current) return
            setPreview(dataUrl)
            onSelect(dataUrl)
        } catch {
            setError("Could not read this image.")
        }
    }

    function onPicked(e: React.ChangeEvent<HTMLInputElement>) {
        const f = e.target.files?.[0];
        e.target.value = "";
        if (f) handleFile(f);
    }

    function onDrop(e: React.DragEvent) {
        e.preventDefault();
        setDragging(false);
        const f = e.dataTransfer.files?.[0];
        if (f) handleFile(f);
    }

    const displayUrl = preview || current;

    return (
        <div className="space-y-2">
            {displayUrl ? (
                <div className="flex items-center gap-3">
                    <div className="size-16 rounded-lg border border-slate-200 overflow-hidden shrink-0 bg-slate-50">
                        <img
                            src={displayUrl}
                            alt=""
                            className="w-full h-full object-cover"
                        />
                    </div>
                    <div className="flex items-center gap-1.5">
                        <button
                            type="button"
                            onClick={() => inputRef.current?.click()}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5"
                        >
                            <ImageIcon className="w-3 h-3" />
                            Replace
                        </button>
                        <button
                            type="button"
                            onClick={() => {
                                setPreview(null)
                                setError(null)
                                onClear()
                            }}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-500 hover:text-red-700 hover:bg-red-50 transition-colors inline-flex items-center gap-1.5"
                        >
                            <TrashIcon className="w-3 h-3" />
                            Remove
                        </button>
                    </div>
                </div>
            ) : (
                <button
                    type="button"
                    onDragOver={(e) => {
                        e.preventDefault();
                        setDragging(true);
                    }}
                    onDragLeave={() => setDragging(false)}
                    onDrop={onDrop}
                    onClick={() => inputRef.current?.click()}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault()
                            inputRef.current?.click()
                        }
                    }}
                    className={cn(
                        "flex flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-6 text-center cursor-pointer transition-colors",
                        dragging
                            ? "border-sky-400 bg-sky-50/50"
                            : "border-slate-200 hover:border-slate-300 hover:bg-slate-50/50"
                    )}
                >
                    <ImageIcon className="w-5 h-5 text-slate-400" />
                    <div>
                        <div className="text-[12px] text-slate-700 font-medium">Drop an image here</div>
                        <div className="text-[11px] text-slate-400 mt-0.5">or click to browse</div>
                    </div>
                    <div className="text-[10px] text-slate-400">PNG, JPG, WebP — max 200 KB</div>
                </button>
            )}
            {error && <div className="text-[11px] text-red-600">{error}</div>}
            <input
                ref={inputRef}
                type="file"
                accept="image/png,image/jpeg,image/jpg,image/webp"
                onChange={onPicked}
                className="hidden"
                aria-hidden="true"
            />
        </div>
    );
}
