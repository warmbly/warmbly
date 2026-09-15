package email

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/config"
)

// An empty signature at the provider is an answer, not a failure. Storing it
// would delete a signature written here.
func TestImportedSignatureEmptyKeepsWhatIsStored(t *testing.T) {
	sig, err := importedSignature("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig != nil {
		t.Errorf("an empty provider signature must not overwrite: %+v", sig)
	}
}

func TestImportedSignatureDerivesPlainText(t *testing.T) {
	sig, err := importedSignature("<p>Hello</p>")
	if err != nil || sig == nil {
		t.Fatalf("no signature: %v", err)
	}
	if sig.HTML != "<p>Hello</p>" || sig.Plain != "Hello" {
		t.Errorf("html=%q plain=%q", sig.HTML, sig.Plain)
	}
}

func TestImportedSignatureTooLargeIsRefused(t *testing.T) {
	huge := "<p>" + strings.Repeat("x", config.SignatureHTMLMax) + "</p>"
	if _, err := importedSignature(huge); err == nil {
		t.Fatal("a signature past the stored maximum must be refused, not truncated")
	}
}

// The limit is in characters, so a signature of multi-byte runes that is well
// inside it must not be refused for its byte length.
func TestImportedSignatureCountsCharacters(t *testing.T) {
	// Three bytes per rune, at half the character limit.
	body := strings.Repeat("あ", config.SignatureHTMLMax/2)
	if _, err := importedSignature("<p>" + body + "</p>"); err != nil {
		t.Fatalf("a signature inside the character limit was refused: %v", err)
	}
}
