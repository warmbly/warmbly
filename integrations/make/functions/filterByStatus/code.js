// Returns only the items whose `status` equals the given value.
// Used by triggers whose list endpoint cannot filter by status server-side
// (e.g. Campaign Completed polls /campaigns, which has no status query param).
function filterByStatus(items, status) {
    if (!Array.isArray(items)) {
        return [];
    }
    return items.filter(function (item) {
        return item && item.status === status;
    });
}
