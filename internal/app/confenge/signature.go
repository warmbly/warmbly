package confenge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Signature is option B: deterministic post-authorization decoration.
// Material content_hash covers channel/recipient/subject/body/purpose only;
// SignatureVersion records which close/CID decoration was applied at authorize/send.
const (
	SignatureImageCID = "tiago-sasaki-signature@confenge"
	// Default on-disk name for the optimized email asset (JPEG, ~15KB).
	SignatureImageFilename = "tiago-sasaki-assinatura.jpeg"
	// Env path override; prefer a pre-optimized JPEG under data/confenge/.
	EnvSignatureImagePath = "CONFENGE_SIGNATURE_IMAGE_PATH"
	// SignatureVersion bumps when the close text or CID decoration semantics change.
	SignatureVersion = "confenge.signature.v2"
)

// First-touch close: plain text only (no image). Cold outreach should not look
// like it has an attachment.
const SignaturePlainBlock = `Abraço,
Eng. Tiago Sasaki
Consultor B2G | Confenge
(48)9 8834-4559`

// Legacy text blocks stripped before re-applying the current first-touch close.
var legacyPlainSignatureTails = []string{
	"\n\n" + SignaturePlainBlock,
	"\n\nAtenciosamente,\n\nEng. Tiago Sasaki\nCONFENGE\ntiago.sasaki@confenge.com.br",
	"\n\nAtenciosamente,\nEng. Tiago Sasaki\nCONFENGE\ntiago.sasaki@confenge.com.br",
	"\n\nAtenciosamente,",
	"\n\nAtenciosamente",
	"\n\nBest Regards,\nEng. Tiago Sasaki",
	"\n\nBest Regards,\n" + "Eng. Tiago Sasaki",
	"\n\nAbraço,\nTiago Sasaki\nCONFENGE",
	"\n\nAbraco,\nTiago Sasaki\nCONFENGE",
	"\n\nAbraço,\nCONFENGE",
	"\n\nAbraco,\nCONFENGE",
	"\n\nAbraço,\nTiago Sasaki",
	"\n\nAbraco,\nTiago Sasaki",
	"\n\nAbraço,\nEng. Tiago Sasaki\nCONFENGE",
}

var (
	sigOnce     sync.Once
	sigBytes    []byte
	sigFilename string
	sigMIME     string
	sigErr      error
)

// signatureCandidates lists preferred light assets first (optimized JPEG), then
// operator-supplied PNG/JPEG names. Used only when HTML embeds the signature CID.
func signatureCandidates() []string {
	candidates := []string{}
	if p := strings.TrimSpace(os.Getenv(EnvSignatureImagePath)); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		filepath.Join("data", "confenge", "tiago-sasaki-assinatura.jpeg"),
		filepath.Join("assets", "confenge", "tiago-sasaki-assinatura.jpeg"),
		filepath.Join("data", "confenge", "tiago-sasaki-assinatura.png"),
		"Tiago Sasaki assinatura.jpeg",
		"Tiago Sasaki assinatura.png",
	)
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "data", "confenge", "tiago-sasaki-assinatura.jpeg"),
			filepath.Join(wd, "data", "confenge", "tiago-sasaki-assinatura.png"),
			filepath.Join(wd, "Tiago Sasaki assinatura.jpeg"),
			filepath.Join(wd, "Tiago Sasaki assinatura.png"),
		)
	}
	return candidates
}

func sniffImage(path string, b []byte) (filename, mime string) {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		if ext == "" || ext == ".jpeg" || ext == ".jpg" {
			return preferExt(base, ".jpeg"), "image/jpeg"
		}
		return base, "image/jpeg"
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return preferExt(base, ".png"), "image/png"
	case ext == ".png":
		return base, "image/png"
	default:
		return preferExt(base, ".jpeg"), "image/jpeg"
	}
}

func preferExt(base, ext string) string {
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "tiago-sasaki-assinatura" + ext
	}
	return strings.TrimSuffix(base, filepath.Ext(base)) + ext
}

func loadSignatureOnce() {
	sigOnce.Do(func() {
		var lastErr error
		for _, p := range signatureCandidates() {
			b, err := os.ReadFile(p)
			if err != nil {
				lastErr = err
				continue
			}
			if len(b) == 0 {
				continue
			}
			fn, mime := sniffImage(p, b)
			sigBytes = b
			sigFilename = fn
			sigMIME = mime
			return
		}
		sigErr = lastErr
		if sigErr == nil {
			sigErr = fmt.Errorf("CONFENGE signature image not found (set %s to optimized JPEG under data/confenge/)", EnvSignatureImagePath)
		}
	})
}

// LoadSignatureJPEG returns signature image bytes (JPEG or PNG). Name kept for callers.
// Only used when outbound HTML embeds SignatureImageCID (not first-touch).
func LoadSignatureJPEG() ([]byte, error) {
	loadSignatureOnce()
	if len(sigBytes) == 0 {
		return nil, sigErr
	}
	return sigBytes, nil
}

// SignatureImageMeta returns filename and MIME for the inline CID part.
func SignatureImageMeta() (filename, mime string, err error) {
	loadSignatureOnce()
	if len(sigBytes) == 0 {
		return "", "", sigErr
	}
	if sigFilename == "" {
		sigFilename = SignatureImageFilename
	}
	if sigMIME == "" {
		sigMIME = "image/jpeg"
	}
	return sigFilename, sigMIME, nil
}

// SignaturePlain returns the first-touch plain-text close (no image).
func SignaturePlain() string { return SignaturePlainBlock }

// SignatureHTML returns the first-touch HTML close as text only (no CID image).
// Avoids paperclip/attachment indicators in Gmail and similar clients.
func SignatureHTML() string {
	var b strings.Builder
	b.WriteString(`<div style="margin-top:16px;font-family:Arial,Helvetica,sans-serif;font-size:13px;color:#1e293b;line-height:1.45">`)
	b.WriteString(`<p style="margin:0">`)
	b.WriteString(strings.ReplaceAll(htmlEscape(SignaturePlainBlock), "\n", "<br>\n"))
	b.WriteString(`</p>`)
	b.WriteString(`</div>`)
	return b.String()
}

// SignatureHTMLWithImage returns Atenciosamente + CID image for non-first-touch
// HTML that intentionally wants the visual signature.
func SignatureHTMLWithImage() string {
	var b strings.Builder
	b.WriteString(`<div style="margin-top:16px;font-family:Arial,Helvetica,sans-serif;font-size:13px;color:#1e293b;line-height:1.45">`)
	b.WriteString(`<p style="margin:0 0 8px 0">Atenciosamente,</p>`)
	b.WriteString(fmt.Sprintf(
		`<p style="margin:0"><img src="cid:%s" alt="Assinatura Tiago Sasaki" width="400" style="max-width:100%%;height:auto;border:0;display:block" /></p>`,
		SignatureImageCID,
	))
	b.WriteString(`</div>`)
	return b.String()
}

// stripLegacyPlainSignature removes old text signatures / draft closings.
func stripLegacyPlainSignature(body string) string {
	body = strings.TrimRight(body, " \t\r\n")
	for {
		trimmed := false
		for _, tail := range legacyPlainSignatureTails {
			if strings.HasSuffix(body, tail) {
				body = strings.TrimSpace(strings.TrimSuffix(body, tail))
				trimmed = true
			}
		}
		if !trimmed {
			break
		}
	}
	return body
}

// AppendSignaturePlain ensures a single first-touch text close (no image).
func AppendSignaturePlain(body string) string {
	body = stripLegacyPlainSignature(body)
	if strings.HasSuffix(body, SignaturePlainBlock) {
		return body
	}
	// Already has the phone line from a prior append.
	if strings.Contains(body, "(48)9 8834-4559") && strings.Contains(body, "Eng. Tiago Sasaki") {
		return body
	}
	return body + "\n\n" + SignaturePlain()
}

// BodyToHTML wraps plain PT-BR body as simple HTML paragraphs and appends the
// first-touch text signature (no CID image).
func BodyToHTML(plainBody string) string {
	plain := stripLegacyPlainSignature(strings.TrimSpace(plainBody))
	if idx := strings.LastIndex(plain, SignaturePlainBlock); idx >= 0 {
		plain = strings.TrimSpace(plain[:idx])
	}
	paras := strings.Split(plain, "\n\n")
	var b strings.Builder
	b.WriteString(`<div style="font-family:Arial,Helvetica,sans-serif;font-size:14px;color:#0f172a;line-height:1.5">`)
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.ReplaceAll(htmlEscape(p), "\n", "<br>\n")
		b.WriteString("<p style=\"margin:0 0 12px 0\">")
		b.WriteString(p)
		b.WriteString("</p>\n")
	}
	b.WriteString(SignatureHTML())
	b.WriteString(`</div>`)
	return b.String()
}

func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return r.Replace(s)
}
