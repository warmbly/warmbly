import ContactsTable from "@/components/app/contacts/ContactsTable";
import AdvisorStrip from "@/components/app/advisor/AdvisorStrip";
import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";

export default function ContactsPage() {
    const canView = usePermission("VIEW_CONTACTS");
    if (!canView) return <NoAccess feature="contacts" permissionLabel="View contacts" />;
    return (
        <>
            {/* List-hygiene findings live where the list does. */}
            <AdvisorStrip surface="contacts" className="px-3 pt-2.5" limit={3} />
            <ContactsTable />
        </>
    );
}
