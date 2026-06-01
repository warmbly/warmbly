package passkey

import "github.com/google/uuid"

// aaguidNames maps well-known authenticator AAGUIDs to a friendly provider
// name. Used ONLY to label a passkey in the manager UI — never for a security
// decision, since an AAGUID can be spoofed without valid attestation (which we
// deliberately don't request for consumer passkeys).
var aaguidNames = map[string]string{
	"08987058-cadc-4b81-b6e1-30de50dcbe96": "Windows Hello",
	"9ddd1817-af5a-4672-a2b9-3e3dd95000a9": "Windows Hello",
	"6028b017-b1d4-4c02-b4b3-afcdafc96bb2": "Windows Hello",
	"dd4ec289-e01d-41c9-bb89-70fa845d4bf2": "iCloud Keychain (Managed)",
	"fbfc3007-154e-4ecc-8c0b-6e020557d7bd": "iCloud Keychain",
	"ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4": "Google Password Manager",
	"adce0002-35bc-c60a-648b-0b25f1f05503": "Chrome on Mac",
	"b5397666-4885-aa6b-cebf-e52262a439a2": "Chromium Browser",
	"bada5566-a7aa-401f-bd96-45619a55120d": "1Password",
	"b84e4048-15dc-4dd0-8640-f4f60813c8af": "NordPass",
	"531126d6-e717-415c-9320-3d9aa6981239": "Dashlane",
	"0ea242b4-43c4-4a1b-8b17-dd6d0b6baec6": "Keeper",
	"f3809540-7f14-49c1-a8b3-8f813b225541": "Enpass",
	"891494da-2c90-4d31-a9d4-4eb0676a53d9": "Proton Pass",
	"d548826e-79b4-db40-a3d8-11116f7e8349": "Bitwarden",
}

// providerName returns a friendly authenticator name for an AAGUID, or "" if
// unknown so the caller can fall back to a generic default.
func providerName(aaguid []byte) string {
	if len(aaguid) != 16 {
		return ""
	}
	id, err := uuid.FromBytes(aaguid)
	if err != nil {
		return ""
	}
	return aaguidNames[id.String()]
}
