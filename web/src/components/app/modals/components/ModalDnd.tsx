import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { closestCenter, DndContext, PointerSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import React from "react";
import toast from "react-hot-toast";

export default function ModalDnd({
    items,
    title,
    description,
    onMove,
    children,
}: {
    items: string[],
    title: string,
    description: string,
    onMove: (id: string, position: number) => Promise<unknown>,
    children: React.ReactNode,
}) {
    const [load, setLoad] = React.useState<boolean>(false);

    const sensors = useSensors(
        useSensor(PointerSensor, {
            activationConstraint: { distance: 5 },
            disabled: load,
        })
    );

    const handleDragEnd = async (event: DragEndEvent) => {
        if (event.active.id === event.over?.id) return;

        const newIndex = items.findIndex((t) => t === event.over?.id);

        try {
            setLoad(true);
            toast.promise(
                onMove(event.active.id.toString(), newIndex),
                {
                    loading: "Reordering...",
                    success: "Successfully updated!",
                    error: (err: AppError) => buildError(err),
                }
            )
        } finally {
            setLoad(false)
        }
    }
    return (
        <>
            <h1 className='text-slate-600 font-inter font-bold text-4xl mb-8'>{title}</h1>
            <p className='text-slate-500 text-lg font-inter mb-10'>{description}</p>
            <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={handleDragEnd}
            >
                <SortableContext
                    items={items}
                    strategy={verticalListSortingStrategy}
                >
                    <div className="space-y-2">
                        {children}
                    </div>
                </SortableContext>
            </DndContext>
        </>
    )
}
