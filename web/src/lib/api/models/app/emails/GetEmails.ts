import type Pagination from "../Pagination";
import type Inbox from "./Inbox";

export default interface GetEmails {
    data: Inbox[];
    pagination: Pagination;
}
