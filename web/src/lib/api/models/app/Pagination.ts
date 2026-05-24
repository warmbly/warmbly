export default interface Pagination {
    total: number | null;
    next_cursor: string | null;
    has_more: boolean;
}
