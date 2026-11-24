package aitools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// recordingAPIKeys stores keys in memory and counts updates.
type recordingAPIKeys struct {
	apikey.APIKeyService
	keys    map[uuid.UUID]*models.APIKey
	updates int
}

func (f *recordingAPIKeys) Get(_ context.Context, orgID, keyID uuid.UUID) (*models.APIKey, *errx.Error) {
	k := f.keys[keyID]
	if k == nil || k.OrganizationID != orgID {
		return nil, errx.New(errx.NotFound, "not found")
	}
	return k, nil
}

func (f *recordingAPIKeys) Update(_ context.Context, orgID, keyID uuid.UUID, data *models.UpdateAPIKey) (*models.APIKey, *errx.Error) {
	k := f.keys[keyID]
	if k == nil || k.OrganizationID != orgID {
		return nil, errx.New(errx.NotFound, "not found")
	}
	f.updates++
	if data.Name != nil {
		k.Name = *data.Name
	}
	if data.Description != nil {
		k.Description = data.Description
	}
	return k, nil
}

func TestUpdateAPIKeyStaysInsideCallerKey(t *testing.T) {
	org := uuid.New()
	callerID := uuid.New()
	wideID := uuid.New()
	narrowID := uuid.New()
	box := uuid.New()
	keys := &recordingAPIKeys{keys: map[uuid.UUID]*models.APIKey{
		callerID: {ID: callerID, OrganizationID: org, Name: "caller", Permissions: models.APIPermReadContacts | models.APIPermAPIKeys},
		wideID:   {ID: wideID, OrganizationID: org, Name: "wide", Permissions: models.APIPermSendCampaigns | models.APIPermAPIKeys},
		narrowID: {ID: narrowID, OrganizationID: org, Name: "narrow", Permissions: models.APIPermReadContacts},
	}}
	r := NewRegistry()
	Deps{APIKeys: keys}.registerAPIKeyTools(r)

	keyCaller := Invocation{
		OrgID: org, IsAPIKey: true, ActsForMember: true, CallerKeyID: callerID,
		APIPerms: models.APIPermAPIKeys, OrgPerms: models.PermManageAPIKeys,
	}
	rename := func(id uuid.UUID) json.RawMessage {
		return json.RawMessage(`{"key_id":"` + id.String() + `","name":"renamed"}`)
	}

	if _, err := r.Call(context.Background(), keyCaller, "update_api_key", rename(wideID)); err == nil || !strings.Contains(err.Error(), "cannot hold permissions") {
		t.Fatalf("wider key renamed: %v", err)
	}
	if keys.updates != 0 {
		t.Fatalf("update ran %d times after a refused rename", keys.updates)
	}

	out, err := r.Call(context.Background(), keyCaller, "update_api_key", rename(narrowID))
	if err != nil {
		t.Fatalf("key inside the caller was refused: %v", err)
	}
	if keys.updates != 1 || keys.keys[narrowID].Name != "renamed" || !strings.Contains(out, "renamed") {
		t.Fatalf("narrow rename: updates=%d name=%q out=%s", keys.updates, keys.keys[narrowID].Name, out)
	}

	unlimited := uuid.New()
	keys.keys[callerID].AllowedEmailAccounts = []uuid.UUID{box}
	keys.keys[unlimited] = &models.APIKey{ID: unlimited, OrganizationID: org, Name: "open", Permissions: models.APIPermReadContacts}
	if _, err := r.Call(context.Background(), keyCaller, "update_api_key", rename(unlimited)); err == nil || !strings.Contains(err.Error(), "mailboxes") {
		t.Fatalf("unlimited key renamed by a mailbox-limited key: %v", err)
	}
	if keys.updates != 1 {
		t.Fatalf("mailbox refusal still wrote: updates=%d", keys.updates)
	}

	session := Invocation{OrgID: org, OrgPerms: models.PermManageAPIKeys, FreshAuthed: true}
	if _, err := r.Call(context.Background(), session, "update_api_key", rename(wideID)); err != nil {
		t.Fatalf("session rename: %v", err)
	}
	if keys.keys[wideID].Name != "renamed" {
		t.Fatalf("session left the name %q", keys.keys[wideID].Name)
	}

	anonymous := keyCaller
	anonymous.CallerKeyID = uuid.Nil
	if _, err := r.Call(context.Background(), anonymous, "update_api_key", rename(narrowID)); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("key without an id was accepted: %v", err)
	}
}
