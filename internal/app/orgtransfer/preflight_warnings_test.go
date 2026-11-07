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
