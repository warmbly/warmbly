export default interface BulkEditContactsField {
    type: "ADD" | "EDIT" | "DELETE" | "RENAME";
    key: string;
    value: string;
}
