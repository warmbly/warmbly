package confenge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignaturePlainFirstTouchText(t *testing.T) {
	s := SignaturePlain()
	for _, want := range []string{
		"Abraço,",
		"Eng. Tiago Sasaki",
		"Consultor B2G | Confenge",
		"(48)9 8834-4559",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("plain signature missing %q: %s", want, s)
		}
	}
	for _, ban := range []string{"Best Regards", "Atenciosamente", "tiago.sasaki@"} {
		if strings.Contains(s, ban) {
			t.Fatalf("plain signature must not contain %q: %s", ban, s)
		}
	}
}

func TestSignatureHTMLFirstTouchNoCID(t *testing.T) {
	html := SignatureHTML()
	if !strings.Contains(html, "Abraço,") || !strings.Contains(html, "Eng. Tiago Sasaki") {
		t.Fatalf("HTML missing first-touch close: %s", html)
	}
	if !strings.Contains(html, "(48)9 8834-4559") {
		t.Fatal("HTML missing phone")
	}
	if strings.Contains(html, "cid:") || strings.Contains(html, SignatureImageCID) {
		t.Fatalf("first-touch HTML must not embed CID image: %s", html)
	}
	if strings.Contains(html, "Best Regards") {
		t.Fatal("Best Regards not allowed")
	}
}

func TestBodyToHTMLFirstTouchNoImage(t *testing.T) {
	html := BodyToHTML("Olá Ana,\n\nNotei a prorrogação do contrato.\n\nFaz sentido conversarmos?")
	if !strings.Contains(html, "Olá Ana") {
		t.Fatal("HTML lost body text")
	}
	if !strings.Contains(html, "Abraço,") || !strings.Contains(html, "Consultor B2G") {
		t.Fatalf("HTML missing first-touch signature: %s", html)
	}
	if strings.Contains(html, "cid:") || strings.Contains(html, SignatureImageCID) {
		t.Fatalf("first-touch must not include CID: %s", html)
	}
}

func TestBodyToHTMLStripsLegacyAndAppliesFirstTouch(t *testing.T) {
	plain := "Olá,\n\nTexto.\n\nAtenciosamente,\n\nEng. Tiago Sasaki\nCONFENGE\ntiago.sasaki@confenge.com.br"
	html := BodyToHTML(plain)
	if strings.Contains(html, "tiago.sasaki@confenge.com.br") || strings.Contains(html, "Atenciosamente") {
		t.Fatalf("legacy close should be replaced: %s", html)
	}
	if !strings.Contains(html, "(48)9 8834-4559") {
		t.Fatal("expected first-touch phone close")
	}
}

func TestLoadSignatureJPEGFromRepoAsset(t *testing.T) {
	// Image still available for optional non-first-touch CID use.
	p := filepath.Join("data", "confenge", "tiago-sasaki-assinatura.jpeg")
	if _, err := os.Stat(p); err != nil {
		t.Skip("optimized signature jpeg not present in test cwd")
	}
	t.Setenv(EnvSignatureImagePath, p)
	b, err := LoadSignatureJPEG()
	if err != nil {
		t.Fatalf("LoadSignatureJPEG: %v", err)
	}
	if len(b) < 1000 {
		t.Fatalf("signature image too small: %d bytes", len(b))
	}
	if len(b) > 40*1024 {
		t.Fatalf("signature too heavy for email: %d bytes", len(b))
	}
	if b[0] != 0xff || b[1] != 0xd8 {
		t.Fatalf("expected JPEG for email asset")
	}
}

func TestAppendSignaturePlainIdempotent(t *testing.T) {
	body := "Olá,\n\nTexto."
	once := AppendSignaturePlain(body)
	twice := AppendSignaturePlain(once)
	if strings.Count(twice, "Eng. Tiago Sasaki") != 1 {
		t.Fatalf("name should appear once, got:\n%s", twice)
	}
	if strings.Count(twice, "(48)9 8834-4559") != 1 {
		t.Fatalf("phone should appear once, got:\n%s", twice)
	}
	if strings.Contains(twice, "Best Regards") || strings.Contains(twice, "cid:") {
		t.Fatalf("unexpected content:\n%s", twice)
	}
}

func TestAppendSignaturePlainReplacesLegacyAbraçoBlock(t *testing.T) {
	body := "Olá,\n\nTexto.\n\nAbraço,\nTiago Sasaki\nCONFENGE"
	got := AppendSignaturePlain(body)
	if strings.Contains(got, "Tiago Sasaki\nCONFENGE") && !strings.Contains(got, "Consultor B2G") {
		t.Fatalf("legacy Abraço block should be replaced:\n%s", got)
	}
	if !strings.Contains(got, "Consultor B2G | Confenge") {
		t.Fatalf("expected first-touch title line:\n%s", got)
	}
}

func TestSignatureHTMLWithImageStillHasCID(t *testing.T) {
	html := SignatureHTMLWithImage()
	if !strings.Contains(html, "cid:"+SignatureImageCID) {
		t.Fatal("optional image HTML should keep CID")
	}
}
