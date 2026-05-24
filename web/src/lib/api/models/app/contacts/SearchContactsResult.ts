import type Pagination from "../Pagination";
import type Contact from "./Contact";

export default interface SearchContactsResult {
    data: Contact[];
    pagination: Pagination;
}
