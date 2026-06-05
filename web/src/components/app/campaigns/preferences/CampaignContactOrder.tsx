// Contact ordering settings — how the campaign picks who to send to next.
// On-theme: slate/sky, rounded-md, 12.5px base, house primitives.

import { useState, useCallback, useRef } from "react";
import {
    DndContext,
    closestCenter,
    KeyboardSensor,
    PointerSensor,
    useSensor,
    useSensors,
    type DragEndEvent,
} from "@dnd-kit/core";
import {
    arrayMove,
    SortableContext,
    sortableKeyboardCoordinates,
    useSortable,
    verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, ChevronDownIcon, GripVerticalIcon, InfoIcon } from "lucide-react";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, TextInput } from "@/components/ui/field";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import { Segmented, SettingRow } from "./components/CampaignPreferenceBoolBox";

const ORDER_OPTIONS = [
    { value: "created_at", label: "Creation time", description: "Order by when contacts were added" },
    { value: "email", label: "Email", description: "Alphabetical by email address" },
    { value: "name", label: "Name", description: "Alphabetical by first, then last name" },
    { value: "custom_field", label: "Custom field", description: "Order by a custom contact field" },
    { value: "manual", label: "Manual", description: "Drag and drop to set custom order" },
] as const;

interface ContactItem {
    id: string;
    email: string;
    firstName: string;
    lastName: string;
    position?: number;
}

function SortableContact({ contact, index }: { contact: ContactItem; index: number }) {
    const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
        id: contact.id,
    });

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
    };

    return (
        <div
            ref={setNodeRef}
            style={style}
            className={`flex items-center gap-3 px-3 h-11 bg-white border border-slate-200 rounded-md ${
                isDragging ? "shadow-md z-10" : ""
            }`}
        >
            <button
                className="cursor-grab active:cursor-grabbing text-slate-400 hover:text-slate-600"
                {...attributes}
                {...listeners}
                aria-label="Drag to reorder"
            >
                <GripVerticalIcon className="w-3.5 h-3.5" />
            </button>
            <span className="font-mono text-[10.5px] text-slate-400 tabular-nums w-5 shrink-0">
                {index + 1}
            </span>
            <div className="flex-1 min-w-0">
                <p className="text-[12.5px] font-medium text-slate-900 truncate">
                    {contact.firstName || contact.lastName
                        ? `${contact.firstName} ${contact.lastName}`.trim()
                        : "Unknown"}
                </p>
                <p className="text-[11px] text-slate-400 truncate">{contact.email}</p>
            </div>
        </div>
    );
}

interface CampaignContactOrderProps {
    campaign: Campaign;
    newCampaign: Campaign;
    setNewCampaign: React.Dispatch<React.SetStateAction<Campaign>>;
    contacts?: ContactItem[];
    onContactsReorder?: (contacts: ContactItem[]) => void;
}

export default function CampaignContactOrder({
    newCampaign,
    setNewCampaign,
    contacts = [],
    onContactsReorder,
}: CampaignContactOrderProps) {
    const [orderedContacts, setOrderedContacts] = useState<ContactItem[]>(
        [...contacts].sort((a, b) => (a.position ?? 0) - (b.position ?? 0)),
    );

    const sensors = useSensors(
        useSensor(PointerSensor),
        useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
    );

    const selectedOption =
        ORDER_OPTIONS.find((o) => o.value === newCampaign.contact_order_by) || ORDER_OPTIONS[0];

    // Order-by dropdown (house framer-motion pattern + flip placement).
    const [orderOpen, setOrderOpen] = useState(false);
    const orderRef = useRef<HTMLDivElement>(null);
    const orderTriggerRef = useRef<HTMLDivElement>(null);
    useClickOutside(orderRef, () => setOrderOpen(false));
    const orderPlacement = useFlipPlacement(orderTriggerRef, orderOpen, 300);

    const handleDragEnd = useCallback(
        (event: DragEndEvent) => {
            const { active, over } = event;
            if (over && active.id !== over.id) {
                setOrderedContacts((items) => {
                    const oldIndex = items.findIndex((i) => i.id === active.id);
                    const newIndex = items.findIndex((i) => i.id === over.id);
                    const updatedItems = arrayMove(items, oldIndex, newIndex).map((item, index) => ({
                        ...item,
                        position: index,
                    }));
                    onContactsReorder?.(updatedItems);
                    return updatedItems;
                });
            }
        },
        [onContactsReorder],
    );

    return (
        <div className="space-y-6">
            {/* Order By */}
            <div>
                <Label>Order contacts by</Label>
                <div ref={orderRef} className="relative w-full max-w-[280px]">
                    <div ref={orderTriggerRef}>
                        <button
                            type="button"
                            onClick={() => setOrderOpen((o) => !o)}
                            className="flex h-8 w-full items-center justify-between rounded-md border border-slate-200 bg-white px-2.5 text-[12.5px] text-slate-800 transition-colors hover:border-slate-300 focus:border-sky-400 focus:outline-none focus:ring-2 focus:ring-sky-100"
                        >
                            <span>{selectedOption.label}</span>
                            <ChevronDownIcon
                                className={`w-3.5 h-3.5 shrink-0 text-slate-400 transition-transform ${orderOpen ? "rotate-180" : ""}`}
                            />
                        </button>
                    </div>
                    <AnimatePresence>
                        {orderOpen && (
                            <motion.div
                                initial={{ opacity: 0, y: orderPlacement === "top" ? 4 : -4 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: orderPlacement === "top" ? 4 : -4 }}
                                transition={{ duration: 0.12 }}
                                className={`absolute left-0 right-0 z-30 overflow-hidden rounded-md border border-slate-200 bg-white py-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] ${
                                    orderPlacement === "top" ? "bottom-full mb-1" : "top-full mt-1"
                                }`}
                            >
                                {ORDER_OPTIONS.map((option) => {
                                    const sel = newCampaign.contact_order_by === option.value;
                                    return (
                                        <button
                                            key={option.value}
                                            type="button"
                                            onClick={() => {
                                                setNewCampaign((prev) => ({ ...prev, contact_order_by: option.value }));
                                                setOrderOpen(false);
                                            }}
                                            className="flex w-full items-start gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-slate-100"
                                        >
                                            <span
                                                className={`mt-0.5 flex size-3.5 shrink-0 items-center justify-center rounded-full border ${
                                                    sel ? "border-sky-600 bg-sky-600" : "border-slate-300 bg-white"
                                                }`}
                                            >
                                                {sel && <CheckIcon className="w-2 h-2 text-white" />}
                                            </span>
                                            <span className="flex flex-col gap-0.5">
                                                <span className="text-[12.5px] font-medium text-slate-900">{option.label}</span>
                                                <span className="text-[11px] text-slate-400">{option.description}</span>
                                            </span>
                                        </button>
                                    );
                                })}
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>
            </div>

            {/* Direction (not for manual) */}
            {newCampaign.contact_order_by !== "manual" && (
                <SettingRow
                    title="Direction"
                    description={
                        newCampaign.contact_order_dir === "desc"
                            ? "Z → A / newest → oldest"
                            : "A → Z / oldest → newest"
                    }
                    control={
                        <Segmented
                            value={newCampaign.contact_order_dir}
                            onChange={(v) =>
                                setNewCampaign((prev) => ({ ...prev, contact_order_dir: v }))
                            }
                            options={[
                                { value: "asc", label: "Ascending" },
                                { value: "desc", label: "Descending" },
                            ]}
                        />
                    }
                />
            )}

            {/* Custom field name */}
            {newCampaign.contact_order_by === "custom_field" && (
                <div>
                    <Label>Custom field name</Label>
                    <TextInput
                        value={newCampaign.contact_order_field || ""}
                        placeholder="e.g. company_size, priority"
                        onChange={(v) =>
                            setNewCampaign((prev) => ({ ...prev, contact_order_field: v }))
                        }
                        className="w-full max-w-[280px]"
                    />
                    <p className="text-[11px] text-slate-400 mt-1.5">
                        Enter the name of a custom field from your contacts.
                    </p>
                </div>
            )}

            {/* Manual drag-and-drop */}
            {newCampaign.contact_order_by === "manual" && (
                <div>
                    <Label>Drag to reorder contacts</Label>
                    {orderedContacts.length > 0 ? (
                        <DndContext
                            sensors={sensors}
                            collisionDetection={closestCenter}
                            onDragEnd={handleDragEnd}
                        >
                            <SortableContext
                                items={orderedContacts.map((c) => c.id)}
                                strategy={verticalListSortingStrategy}
                            >
                                <div className="space-y-1.5 max-h-96 overflow-y-auto pr-1">
                                    {orderedContacts.map((contact, index) => (
                                        <SortableContact key={contact.id} contact={contact} index={index} />
                                    ))}
                                </div>
                            </SortableContext>
                        </DndContext>
                    ) : (
                        <div className="text-center py-8 border border-dashed border-slate-200 rounded-md">
                            <p className="text-[12.5px] text-slate-700 font-medium">
                                No contacts in this campaign
                            </p>
                            <p className="text-[11px] text-slate-400 mt-1">
                                Add contacts to enable manual ordering.
                            </p>
                        </div>
                    )}
                </div>
            )}

            {/* Info */}
            <div className="flex items-start gap-2 rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5">
                <InfoIcon className="w-3.5 h-3.5 text-slate-400 mt-0.5 shrink-0" />
                <p className="text-[11.5px] text-slate-600 leading-relaxed">
                    When processing sends, contacts are selected in this order — it determines who
                    receives emails first while the campaign is running.
                </p>
            </div>
        </div>
    );
}
