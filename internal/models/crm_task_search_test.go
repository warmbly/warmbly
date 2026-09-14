package models

import "testing"

func strptr(v string) *string { return &v }

// The id facets are compared against uuid columns, so a malformed one has to be
// refused before it becomes an error from the driver halfway through a query.
func TestSearchTasksValidateRefusesIdsThatAreNotIds(t *testing.T) {
	good := "8f14e45f-ceea-467a-9f2a-4d29f9f0f5f3"
	for _, tc := range []struct {
		name string
		in   SearchTasks
		ok   bool
	}{
		{name: "empty body", in: SearchTasks{}, ok: true},
		{name: "real ids", in: SearchTasks{
			AssignedTo: []string{good},
			ContactID:  strptr(good),
			DealID:     strptr(good),
		}, ok: true},
		{name: "absent id is not a filter", in: SearchTasks{ContactID: nil}, ok: true},
		{name: "blank id is not a filter", in: SearchTasks{ContactID: strptr("  ")}, ok: true},
		{name: "assignee is a name", in: SearchTasks{AssignedTo: []string{"me"}}, ok: false},
		{name: "one bad assignee among good ones", in: SearchTasks{
			AssignedTo: []string{good, "nonsense"},
		}, ok: false},
		{name: "blank assignee", in: SearchTasks{AssignedTo: []string{""}}, ok: false},
		{name: "contact is not an id", in: SearchTasks{ContactID: strptr("42")}, ok: false},
		{name: "deal is not an id", in: SearchTasks{DealID: strptr("deal-1")}, ok: false},
		// Only the uuid columns are checked: these are text and a filter that
		// matches nothing is a legitimate answer, not a bad request.
		{name: "status and type are free text", in: SearchTasks{
			Statuses: []string{"nonsense"}, Types: []string{"nonsense"},
		}, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.ok && err != nil {
				t.Fatalf("refused a valid filter: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("accepted a filter Postgres would reject")
			}
		})
	}
}
