import ContactsTable from "@/components/app/contacts/ContactsTable";
import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";

export default function ContactsPage() {
    const canView = usePermission("VIEW_CONTACTS");
    if (!canView) return <NoAccess feature="contacts" permissionLabel="View contacts" />;
    return <ContactsTable />;
}
