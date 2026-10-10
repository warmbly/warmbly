package models

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func bindQuery(t *testing.T, rawQuery string, dst any) error {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?"+rawQuery, nil)
	return c.ShouldBindQuery(dst)
}

func TestParamUUIDBindsFromQuery(t *testing.T) {
	id := uuid.New()
	var s AdminMailboxSearch
	if err := bindQuery(t, "worker_id="+id.String()+"&org_id="+id.String(), &s); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if s.WorkerID == nil || s.WorkerID.UUID != id || s.OrgID == nil || s.OrgID.UUID != id {
		t.Fatalf("got worker %v org %v, want %s", s.WorkerID, s.OrgID, id)
	}
	if s.UserID != nil {
		t.Fatalf("absent user_id bound to %v", s.UserID)
	}
}

func TestParamUUIDRejectsGarbage(t *testing.T) {
	var s AdminMailboxSearch
	if err := bindQuery(t, "worker_id=not-a-uuid", &s); err == nil {
		t.Fatal("bound a malformed uuid")
	}
}

func TestAdminMailboxExactIdentityBindsAndRejectsInvalidUUID(t *testing.T) {
	id := uuid.New()
	var search AdminMailboxSearch
	if err := bindQuery(t, "mailbox_id="+id.String()+"&status=all", &search); err != nil {
		t.Fatal(err)
	}
	if search.MailboxID == nil || search.MailboxID.UUID != id || search.Status != "all" || search.WorkerID != nil {
		t.Fatalf("exact mailbox lookup changed identity or placement scope: %+v", search)
	}
	if err := bindQuery(t, "mailbox_id=not-a-uuid", &AdminMailboxSearch{}); err == nil {
		t.Fatal("invalid exact mailbox identity accepted")
	}
}

// Every admin search struct must bind its ids and cursor together, so a field
// typed as a bare uuid.UUID cannot 400 an explorer again.
func TestAdminSearchStructsBindIDsAndCursor(t *testing.T) {
	id := uuid.NewString()
	for _, dst := range []any{
		&AdminUserSearch{}, &AdminCampaignSearch{}, &AdminAuditLogSearch{}, &AdminMailboxSearch{},
		&AdminOrgSearch{}, &AdminLimitRequestSearch{}, &AdminOutreachSearch{}, &AdminDiscountSearch{},
	} {
		typ := reflect.TypeOf(dst).Elem()
		query := ""
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			key := f.Tag.Get("form")
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft == reflect.TypeOf(uuid.UUID{}) {
				t.Errorf("%s.%s is a uuid.UUID, which gin cannot bind; use ParamUUID", typ.Name(), f.Name)
			}
			if ft == reflect.TypeOf(ParamUUID{}) {
				query += key + "=" + id + "&"
			}
		}
		query += "cursor=o1_NTA"
		if err := bindQuery(t, query, dst); err != nil {
			t.Errorf("%s: bind %q: %v", typ.Name(), query, err)
		}
	}
}
