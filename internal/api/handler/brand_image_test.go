package handler

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func encodedImage(t *testing.T, format string, side int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	var err error
	if format == "jpg" {
		err = jpeg.Encode(&buf, img, nil)
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadImage(t *testing.T, lim imageLimits, body []byte) (mime, ext, code string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "upload")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(body)
	_ = mw.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/upload", &buf)
	c.Request.Header.Set("Content-Type", mw.FormDataContentType())
	_, mime, ext, xerr := readImageUpload(c, lim)
	if xerr != nil {
		return "", "", xerr.Identifier
	}
	return mime, ext, ""
}

// The tab icon is a small PNG; the sign-in image is a PNG or JPG up to a full page.
func TestAgencyBrandImageLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if mime, ext, code := uploadImage(t, brandIconLimits, encodedImage(t, "png", 512)); code != "" || mime != "image/png" || ext != ".png" {
		t.Fatalf("512px PNG icon: %q %q %q", mime, ext, code)
	}
	if _, _, code := uploadImage(t, brandIconLimits, encodedImage(t, "png", 513)); code != "image_dimensions_too_large" {
		t.Fatalf("513px icon: %q", code)
	}
	if _, _, code := uploadImage(t, brandIconLimits, encodedImage(t, "jpg", 64)); code != "image_format_not_allowed" {
		t.Fatalf("JPG icon: %q", code)
	}
	if _, _, code := uploadImage(t, brandIconLimits, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); code == "" {
		t.Fatal("an SVG icon was accepted")
	}
	if _, _, code := uploadImage(t, brandIconLimits, append(encodedImage(t, "png", 16), make([]byte, 1<<20)...)); code != "image_too_large" {
		t.Fatalf("icon over 1 MB: %q", code)
	}

	if mime, ext, code := uploadImage(t, brandSignInImageLimits, encodedImage(t, "jpg", 2560)); code != "" || mime != "image/jpeg" || ext != ".jpg" {
		t.Fatalf("2560px JPG sign-in image: %q %q %q", mime, ext, code)
	}
	if _, _, code := uploadImage(t, brandSignInImageLimits, encodedImage(t, "png", 1200)); code != "" {
		t.Fatalf("PNG sign-in image: %q", code)
	}
	if _, _, code := uploadImage(t, brandSignInImageLimits, encodedImage(t, "jpg", 2561)); code != "image_dimensions_too_large" {
		t.Fatalf("2561px sign-in image: %q", code)
	}
	if _, _, code := uploadImage(t, brandSignInImageLimits, append(encodedImage(t, "jpg", 16), make([]byte, 4<<20)...)); code != "image_too_large" {
		t.Fatalf("sign-in image over 4 MB: %q", code)
	}

	// A logo keeps the caps it had.
	if _, _, code := uploadImage(t, brandLogoLimits, encodedImage(t, "png", 1025)); code != "image_dimensions_too_large" {
		t.Fatalf("1025px logo: %q", code)
	}
	if _, _, code := uploadImage(t, brandLogoLimits, encodedImage(t, "jpg", 1024)); code != "" {
		t.Fatalf("1024px JPG logo: %q", code)
	}
}

// Each image is stored under its own name, and no two uploads share a key.
func TestBrandAssetKeysAreUniquePerUpload(t *testing.T) {
	owner := uuid.New()
	images := []agencyBrandImage{agencyBrandLogo, agencyBrandIcon, agencyBrandSign}
	seen := map[string]bool{}
	for _, img := range images {
		for range 2 {
			key := brandAssetKey(owner, img.key, ".png")
			if !strings.HasPrefix(key, models.BrandAssetKeyPrefix+owner.String()+"/"+img.key+"-") || !strings.HasSuffix(key, ".png") || seen[key] {
				t.Fatalf("key %q for %s", key, img.image)
			}
			seen[key] = true
			if got := avatarKeyFromURL("https://cdn.test/public/"+key, models.BrandAssetKeyPrefix); got != key {
				t.Fatalf("key not recovered from its URL: %q", got)
			}
		}
	}
	if agencyBrandLogo.audit == agencyBrandIcon.audit || agencyBrandIcon.audit == agencyBrandSign.audit {
		t.Fatal("two images share an audit field")
	}
}
