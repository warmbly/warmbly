package orgtransfer

import (
	"testing"

	"github.com/google/uuid"
)

func TestLiveSyncArrivalQueueAndPendingMapsStayInstanceLocal(t *testing.T) {
	pool := specLivePool(t)
	f := newTenantFixture(t, pool)
	ctx := t.Context()
	mailbox := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider) VALUES($1,$2,$3,$4,'Arrival','','','smtp_imap')`, mailbox, f.owner, f.org, mailbox.String()+"@test.local"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM email_accounts WHERE id=$1`, mailbox) })
	for i, key := range []string{"legacy", "pending", "handled"} {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO email_message_map(user_id,email_id,message_id,id) VALUES($1,$2,$3,$4)`, f.owner, mailbox, key, id); err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			if _, err := pool.Exec(ctx, `INSERT INTO sync_arrival_outbox(user_id,email_id,organization_id,message_id,id,payload,stage) VALUES($1,$2,$3,$4,$5,'fixture',$6)`, f.owner, mailbox, f.org, key, id, i-1); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ExcludedTables["sync_arrival_outbox"] == "" {
		t.Fatal("queue is not explicitly excluded")
	}
	var scope string
	for _, spec := range Tables {
		if spec.Name == "sync_arrival_outbox" {
			t.Fatal("queue secrets or body references transferred")
		}
		if spec.Name == "email_message_map" {
			scope = spec.Scope
		}
	}
	if scope == "" {
		t.Fatal("map scope missing")
	}
	rows, err := pool.Query(ctx, `SELECT message_id FROM email_message_map WHERE `+scope+` ORDER BY message_id`, f.org)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "handled" || keys[1] != "legacy" {
		t.Fatalf("pending maps suppressed destination resync: %v", keys)
	}
}
