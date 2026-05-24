import { useState, useCallback } from 'react';
import {
    DndContext,
    closestCenter,
    KeyboardSensor,
    PointerSensor,
    useSensor,
    useSensors,
    type DragEndEvent,
} from '@dnd-kit/core';
import {
    arrayMove,
    SortableContext,
    sortableKeyboardCoordinates,
    useSortable,
    verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { RiDraggable } from '@remixicon/react';
import type Campaign from '@/lib/api/models/app/campaigns/Campaign';
import SubTitle from '../../text/SubTitle';
import Title from '../../text/Title';
import Selector from '../../popup/select/Selector';
import SelectMenu from '../../popup/select/SelectMenu';
import SelectOption from '../../popup/select/SelectOption';
import MiniInput from '../../popup/MiniInput';
import Switch from '../../Switch';
import CampaignPreferenceBoolBox from './components/CampaignPreferenceBoolBox';

// Order by options
const ORDER_OPTIONS = [
    { value: 'created_at', label: 'Creation Time', description: 'Order by when contacts were added' },
    { value: 'email', label: 'Email', description: 'Alphabetical by email address' },
    { value: 'name', label: 'Name', description: 'Alphabetical by first name, then last name' },
    { value: 'custom_field', label: 'Custom Field', description: 'Order by a custom contact field' },
    { value: 'manual', label: 'Manual', description: 'Drag and drop to set custom order' },
] as const;

interface ContactItem {
    id: string;
    email: string;
    firstName: string;
    lastName: string;
    position?: number;
}

interface SortableContactProps {
    contact: ContactItem;
    index: number;
}

function SortableContact({ contact, index }: SortableContactProps) {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id: contact.id });

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
    };

    return (
        <div
            ref={setNodeRef}
            style={style}
            className={`flex items-center gap-3 px-3 py-2 bg-white border border-gray-200 rounded-lg ${
                isDragging ? 'shadow-lg z-10' : ''
            }`}
        >
            <button
                className="cursor-grab active:cursor-grabbing text-gray-400 hover:text-gray-600"
                {...attributes}
                {...listeners}
            >
                <RiDraggable className="w-5 h-5" />
            </button>
            <span className="text-sm text-gray-500 w-6">{index + 1}.</span>
            <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-gray-900 truncate">
                    {contact.firstName || contact.lastName
                        ? `${contact.firstName} ${contact.lastName}`.trim()
                        : 'Unknown'}
                </p>
                <p className="text-xs text-gray-500 truncate">{contact.email}</p>
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
    campaign,
    newCampaign,
    setNewCampaign,
    contacts = [],
    onContactsReorder,
}: CampaignContactOrderProps) {
    const [showOrderMenu, setShowOrderMenu] = useState(false);
    const [orderedContacts, setOrderedContacts] = useState<ContactItem[]>(
        [...contacts].sort((a, b) => (a.position ?? 0) - (b.position ?? 0))
    );

    const sensors = useSensors(
        useSensor(PointerSensor),
        useSensor(KeyboardSensor, {
            coordinateGetter: sortableKeyboardCoordinates,
        })
    );

    const selectedOption = ORDER_OPTIONS.find(o => o.value === newCampaign.contact_order_by) || ORDER_OPTIONS[0];

    const handleDragEnd = useCallback((event: DragEndEvent) => {
        const { active, over } = event;

        if (over && active.id !== over.id) {
            setOrderedContacts((items) => {
                const oldIndex = items.findIndex((i) => i.id === active.id);
                const newIndex = items.findIndex((i) => i.id === over.id);

                const newItems = arrayMove(items, oldIndex, newIndex);

                // Update positions
                const updatedItems = newItems.map((item, index) => ({
                    ...item,
                    position: index,
                }));

                // Notify parent
                onContactsReorder?.(updatedItems);

                return updatedItems;
            });
        }
    }, [onContactsReorder]);

    return (
        <div className="space-y-6">
            {/* Order By Selection */}
            <div>
                <SubTitle>Order Contacts By</SubTitle>
                <div className="relative">
                    <Selector show={showOrderMenu} setShow={setShowOrderMenu} caret>
                        <span className="text-sm text-gray-700">{selectedOption.label}</span>
                    </Selector>
                    <SelectMenu show={showOrderMenu}>
                        {ORDER_OPTIONS.map((option) => (
                            <SelectOption
                                key={option.value}
                                selected={newCampaign.contact_order_by === option.value}
                                onClick={() => {
                                    setNewCampaign((prev) => ({
                                        ...prev,
                                        contact_order_by: option.value,
                                    }));
                                    setShowOrderMenu(false);
                                }}
                            >
                                <div>
                                    <p className="text-sm font-medium">{option.label}</p>
                                    <p className="text-xs text-gray-500">{option.description}</p>
                                </div>
                            </SelectOption>
                        ))}
                    </SelectMenu>
                </div>
            </div>

            {/* Direction Toggle (not shown for manual) */}
            {newCampaign.contact_order_by !== 'manual' && (
                <CampaignPreferenceBoolBox>
                    <div>
                        <Title>Descending Order</Title>
                        <SubTitle>
                            {newCampaign.contact_order_dir === 'desc'
                                ? 'Contacts will be processed from Z to A / newest to oldest'
                                : 'Contacts will be processed from A to Z / oldest to newest'}
                        </SubTitle>
                    </div>
                    <Switch
                        id="campaign-contact-order-dir"
                        value={newCampaign.contact_order_dir === 'desc'}
                        onChange={(v) =>
                            setNewCampaign((prev) => ({
                                ...prev,
                                contact_order_dir: v ? 'desc' : 'asc',
                            }))
                        }
                    />
                </CampaignPreferenceBoolBox>
            )}

            {/* Custom Field Input */}
            {newCampaign.contact_order_by === 'custom_field' && (
                <div>
                    <SubTitle>Custom Field Name</SubTitle>
                    <MiniInput
                        value={newCampaign.contact_order_field || ''}
                        placeholder="e.g., company_size, priority"
                        onChange={(e) =>
                            setNewCampaign((prev) => ({
                                ...prev,
                                contact_order_field: e.target.value,
                            }))
                        }
                    />
                    <p className="text-xs text-gray-500 mt-1">
                        Enter the name of a custom field from your contacts
                    </p>
                </div>
            )}

            {/* Manual Drag-and-Drop */}
            {newCampaign.contact_order_by === 'manual' && (
                <div>
                    <SubTitle>Drag to Reorder Contacts</SubTitle>
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
                                <div className="space-y-2 max-h-96 overflow-y-auto pr-1">
                                    {orderedContacts.map((contact, index) => (
                                        <SortableContact
                                            key={contact.id}
                                            contact={contact}
                                            index={index}
                                        />
                                    ))}
                                </div>
                            </SortableContext>
                        </DndContext>
                    ) : (
                        <div className="text-center py-8 text-gray-500 border border-dashed border-gray-200 rounded-lg">
                            <p className="text-sm">No contacts in this campaign</p>
                            <p className="text-xs mt-1">Add contacts to enable manual ordering</p>
                        </div>
                    )}
                </div>
            )}

            {/* Info Box */}
            <div className="bg-blue-50 border border-blue-100 rounded-lg p-4">
                <p className="text-sm text-blue-800">
                    <strong>How contact ordering works:</strong> When processing emails,
                    contacts will be selected in the order you specify here. This determines
                    who receives emails first when your campaign is running.
                </p>
            </div>
        </div>
    );
}
