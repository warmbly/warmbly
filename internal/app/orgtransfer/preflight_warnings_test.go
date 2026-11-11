package orgtransfer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

type preflightWarningsRepo struct {
	repository.OrgTransferRepository
}

func (preflightWarningsRepo) ResolveUsersByEmail(context.Context, []string) (map[string]uuid.UUID, error) {
	return map[string]uuid.UUID{}, nil
}

func TestPreflightPreservesWarningArrayWithoutAttachments(t *testing.T) {
	const passphrase = "offline archive fixture passphrase"
	header, _, err := NewSecretsHeader(passphrase)
	if err != nil {
		t.Fatal(err)
	}
	org := uuid.New()
	manifest := Manifest{
		Kind: ArchiveKind, FormatVersion: models.OrgTransferFormatVersion,
		OrganizationID: org, Secrets: header, SourceAppVersion: "v0.6.75",
		Members: []models.OrgArchiveUser{{ID: uuid.New(), Email: "unknown@example.test"}},
	}
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	w, err := zw.Create(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(w).Encode(&manifest); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	creds, err := encrypt.NewEncrypter(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := &service{repo: preflightWarningsRepo{}, creds: creds}
	out, xerr := svc.Preflight(t.Context(), org, bytes.NewReader(data.Bytes()), passphrase)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(out.UnknownMembers) != 1 {
		t.Fatal("fixture must reach warning render with known array length")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	t.Logf("warnings JSON: %s; secrets_unsealed=%v; unknown_members=%d", wire["warnings"], out.SecretsUnsealed, len(out.UnknownMembers))
	if string(wire["warnings"]) != "[]" {
		t.Fatalf("preflight warnings must serialize as an array; expected [], got %s", wire["warnings"])
	}
}

type preflightKnownMembersRepo struct {
	repository.OrgTransferRepository
}

func (preflightKnownMembersRepo) ResolveUsersByEmail(_ context.Context, emails []string) (map[string]uuid.UUID, error) {
	out := make(map[string]uuid.UUID, len(emails))
	for _, e := range emails {
		out[e] = uuid.New()
	}
	return out, nil
}

// Every member having an account here is the common case, and the dashboard reads each list's length.
func TestPreflightSerializesEmptyListsAsArrays(t *testing.T) {
	org := uuid.New()
	manifest := Manifest{
		Kind: ArchiveKind, FormatVersion: models.OrgTransferFormatVersion,
		OrganizationID: org, SourceAppVersion: "v0.6.75",
		Groups:  []models.OrgDataGroup{models.OrgDataGroupCore},
		Members: []models.OrgArchiveUser{{ID: uuid.New(), Email: "owner@example.test"}},
	}
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	w, err := zw.Create(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(w).Encode(&manifest); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	svc := &service{repo: preflightKnownMembersRepo{}}
	out, xerr := svc.Preflight(t.Context(), org, bytes.NewReader(data.Bytes()), "")
	if xerr != nil {
		t.Fatal(xerr)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"unknown_members", "skipped_tables"} {
		if string(wire[key]) != "[]" {
			t.Errorf("%s must serialize as [], got %s", key, wire[key])
		}
	}
}
