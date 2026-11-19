package store

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// memBlobs is an in-memory object store.
type memBlobs struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func newMemBlobs() *memBlobs { return &memBlobs{objs: map[string][]byte{}} }

func (b *memBlobs) Put(_ context.Context, key string, body io.Reader, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objs[key] = data
	return nil
}

func (b *memBlobs) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.objs[key]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *memBlobs) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objs, key)
	return nil
}

func (b *memBlobs) has(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.objs[key]
	return ok
}

// memAvatars records the photos the dashboard was handed, by Warmbly mailbox.
type memAvatars struct {
	mu    sync.Mutex
	saved map[uuid.UUID]int
}

func (a *memAvatars) Save(_ context.Context, _, id uuid.UUID, _ []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.saved == nil {
		a.saved = map[uuid.UUID]int{}
	}
	a.saved[id]++
	return nil
}

func (a *memAvatars) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.saved)
}

// flakyPhotos refuses the first n photo sets with a transient error.
type flakyPhotos struct {
	graphkit.API
	mu   sync.Mutex
	fail int
	sets int
}

func (f *flakyPhotos) SetMailboxPhoto(ctx context.Context, id, name string, data []byte, key string) (*graphkit.Mailbox, error) {
	f.mu.Lock()
	f.sets++
	if f.fail > 0 {
		f.fail--
		f.mu.Unlock()
		return nil, &graphkit.Error{Status: 503, Code: "upstream_unavailable"}
	}
	f.mu.Unlock()
	return f.API.SetMailboxPhoto(ctx, id, name, data, key)
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		for y := range 8 {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: 120, B: uint8(y * 30), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func withPhotos(h *harness) (*memBlobs, *memAvatars) {
	blobs, avatars := newMemBlobs(), &memAvatars{}
	h.s.Blobs = blobs
	h.s.WireAvatars(avatars)
	return blobs, avatars
}

func TestPhotoUploadIsValidatedNormalisedAndScopedToTheWorkspace(t *testing.T) {
	h := newHarness(t)
	blobs, _ := withPhotos(h)
	ctx := context.Background()

	if _, err := h.s.UploadPhoto(ctx, h.org, h.user, []byte("<svg onload=alert(1)>")); code(err) != CodePhotoInvalid {
		t.Fatalf("a non-image upload: %v", err)
	}
	p, err := h.s.UploadPhoto(ctx, h.org, h.user, testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	key := models.StorePhotoKey(h.org, p.ID)
	if !blobs.has(key) || !strings.HasPrefix(key, models.StorePhotoKeyPrefix+h.org.String()+"/") {
		t.Fatalf("stored under %q", key)
	}
	data, err := h.s.PhotoBytes(ctx, h.org, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "jpeg" {
		t.Fatalf("stored photo is %q, %v; want a JPEG", format, err)
	}

	// Another workspace can neither read it nor put it on an order.
	other := uuid.New()
	if _, err := h.s.PhotoBytes(ctx, other, p.ID); err == nil {
		t.Fatal("another workspace read the photo")
	}
	if _, err := h.s.PutRegistrant(ctx, other, testRegistrant()); err != nil {
		t.Fatal(err)
	}
	req := domainAndMailboxes("othercorp.com", graphkit.ProviderGoogle, 1)
	req.Photos = map[string]uuid.UUID{"othercorp.com": p.ID}
	if _, err := h.s.CreateQuote(ctx, other, req); code(err) != CodePhotoNotFound {
		t.Fatalf("a quote naming another workspace's photo: %v", err)
	}

	// A partner-managed workspace has no store, so no photos either.
	h.s.WireManaged(managed{org: h.org})
	if _, err := h.s.UploadPhoto(ctx, h.org, h.user, testPNG(t)); code(err) != CodeManagedByPartner {
		t.Fatalf("upload in a partner-managed workspace: %v", err)
	}
}

func TestQuoteRequestReadsAPhotoPerMailboxLine(t *testing.T) {
	id := uuid.New()
	var q QuoteRequest
	body := `{"items":[{"type":"domain","domain":"acme.com","years":1},
		{"type":"mailboxes","domain":"acme.com","provider":"google","photo_id":"` + id.String() + `","mailboxes":[{"local_part":"ada","first_name":"Ada","last_name":"L"}]}]}`
	if err := json.Unmarshal([]byte(body), &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Items) != 2 || q.Items[1].Mailboxes[0].LocalPart != "ada" || q.Photos["acme.com"] != id {
		t.Fatalf("parsed %+v", q)
	}
	// GraphKit never sees the photo.
	raw, _ := json.Marshal(q.Items)
	if strings.Contains(string(raw), "photo") {
		t.Fatalf("items sent upstream carry the photo: %s", raw)
	}
}

func TestQuoteRequestReadsAPhotoPerMailbox(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	var q QuoteRequest
	body := `{"items":[{"type":"mailboxes","domain":"Acme.com","provider":"google","mailboxes":[
		{"local_part":"ada","first_name":"Ada","last_name":"L","photo_id":"` + a.String() + `"},
		{"local_part":" Bob ","first_name":"Bob","last_name":"K","photo_id":"` + b.String() + `"},
		{"local_part":"cy","first_name":"Cy","last_name":"K"}]}]}`
	if err := json.Unmarshal([]byte(body), &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Photos) != 2 || q.Photos["ada@acme.com"] != a || q.Photos["bob@acme.com"] != b {
		t.Fatalf("photos = %v", q.Photos)
	}
	raw, _ := json.Marshal(q.Items)
	if strings.Contains(string(raw), "photo") {
		t.Fatalf("items sent upstream carry the photo: %s", raw)
	}
}

func TestOnePhotoServesManyMailboxesAndAMailboxPhotoBeatsItsDomain(t *testing.T) {
	h := newHarness(t)
	h.fund(80)
	withPhotos(h)
	ctx := context.Background()
	p1, err := h.s.UploadPhoto(ctx, h.org, h.user, testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := h.s.UploadPhoto(ctx, h.org, h.user, testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	shared, own := p1.ID, p2.ID
	req := domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 3)
	more := domainAndMailboxes("acmehq.com", graphkit.ProviderGoogle, 2)
	req.Items = append(req.Items, more.Items...)
	// One sender's photo on a mailbox of each domain, another sender's on bob@ only.
	req.Photos = map[string]uuid.UUID{"ada@acmeoutreach.com": shared, "cy@acmeoutreach.com": shared, "ada@acmehq.com": shared, "acmehq.com": own, "bob@acmeoutreach.com": own}
	_, o := h.buy(t, req)
	h.settle(t)
	o, err = h.s.GetOrder(ctx, h.org, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uuid.UUID{"ada@acmeoutreach.com": shared, "bob@acmeoutreach.com": own, "cy@acmeoutreach.com": shared, "ada@acmehq.com": shared, "bob@acmehq.com": own}
	if len(o.Mailboxes) != len(want) {
		t.Fatalf("mailboxes = %d", len(o.Mailboxes))
	}
	for _, m := range o.Mailboxes {
		row, _ := h.repo.GetMailbox(ctx, h.org, m.ID)
		if row.PhotoID == nil || *row.PhotoID != want[m.Email] {
			t.Fatalf("%s photo = %v, want %v", m.Email, row.PhotoID, want[m.Email])
		}
	}
	// A photo for an address the order does not buy is refused.
	bad := domainAndMailboxes("acmemail.com", graphkit.ProviderGoogle, 1)
	bad.Photos = map[string]uuid.UUID{"zed@acmemail.com": shared}
	if _, err := h.s.CreateQuote(ctx, h.org, bad); code(err) != CodeInvalidItems {
		t.Fatalf("a photo for an address not on the order: %v", err)
	}
}

func TestOrderPhotoGoesOnEveryMailboxOfTheDomainAndIsRetried(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	_, avatars := withPhotos(h)
	flaky := &flakyPhotos{API: h.fake, fail: 2}
	h.s.setAPI(flaky)
	ctx := context.Background()

	p, err := h.s.UploadPhoto(ctx, h.org, h.user, testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	req := domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 3)
	req.Photos = map[string]uuid.UUID{"acmeoutreach.com": p.ID}
	_, o := h.buy(t, req)
	h.settle(t)

	// Two sets failed; the reconciler tries them again once the retry interval passes.
	for range 3 {
		h.clock.advance(photoRetryAfter + time.Second)
		if err := h.s.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		h.s.connects.Wait()
	}
	o, err = h.s.GetOrder(ctx, h.org, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Mailboxes) != 3 {
		t.Fatalf("mailboxes = %d", len(o.Mailboxes))
	}
	for _, m := range o.Mailboxes {
		if m.Photo != "set" {
			t.Fatalf("%s photo = %q, want set", m.Email, m.Photo)
		}
		row, _ := h.repo.GetMailbox(ctx, h.org, m.ID)
		gk, err := h.fake.GetMailbox(ctx, row.GraphKitMailboxID)
		if err != nil || gk.PhotoURL == nil {
			t.Fatalf("%s has no photo at GraphKit: %+v, %v", m.Email, gk, err)
		}
	}
	if avatars.count() != 3 {
		t.Fatalf("dashboard photos for %d mailboxes, want 3", avatars.count())
	}
	flaky.mu.Lock()
	sets := flaky.sets
	flaky.mu.Unlock()
	if sets < 5 {
		t.Fatalf("photo sets = %d, want the two failures retried", sets)
	}
	// The photo is in use, so the sweep keeps it.
	h.clock.advance(48 * time.Hour)
	if err := h.s.sweepPhotos(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.repo.GetPhoto(ctx, h.org, p.ID); got == nil {
		t.Fatal("the sweep removed a photo mailboxes carry")
	}
}

func TestChangePhotoSetsItAtTheProvider(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	_, avatars := withPhotos(h)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	mbx := o.Mailboxes[0]

	p, err := h.s.UploadPhoto(ctx, h.org, h.user, testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.SetMailboxPhoto(ctx, uuid.New(), mbx.ID, p.ID); err == nil {
		t.Fatal("another workspace changed the photo")
	}
	if _, err := h.s.SetMailboxPhoto(ctx, h.org, mbx.ID, uuid.New()); code(err) != CodePhotoNotFound {
		t.Fatalf("an unknown photo: %v", err)
	}
	got, err := h.s.SetMailboxPhoto(ctx, h.org, mbx.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Photo != "set" || avatars.count() != 1 {
		t.Fatalf("after change: photo %q, dashboard photos %d", got.Photo, avatars.count())
	}
	row, _ := h.repo.GetMailbox(ctx, h.org, mbx.ID)
	if gk, _ := h.fake.GetMailbox(ctx, row.GraphKitMailboxID); gk == nil || gk.PhotoURL == nil {
		t.Fatal("the provider photo was not set")
	}
}

func TestUnusedPhotosAreSweptAfterADay(t *testing.T) {
	h := newHarness(t)
	blobs, _ := withPhotos(h)
	ctx := context.Background()
	p, err := h.s.UploadPhoto(ctx, h.org, h.user, testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	h.clock.advance(23 * time.Hour)
	_ = h.s.sweepPhotos(ctx)
	if got, _ := h.repo.GetPhoto(ctx, h.org, p.ID); got == nil {
		t.Fatal("swept before a day")
	}
	h.clock.advance(2 * time.Hour)
	if err := h.s.sweepPhotos(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.repo.GetPhoto(ctx, h.org, p.ID); got != nil || blobs.has(models.StorePhotoKey(h.org, p.ID)) {
		t.Fatal("an unused photo outlived the sweep")
	}
}

func TestDefaultForward(t *testing.T) {
	for _, c := range []struct{ website, email, want string }{
		{"https://acme.com", "ada@gmail.com", "https://acme.com"},
		{"acme.com", "", "https://acme.com"},
		{"http://acme.com/about", "", "https://acme.com/about"},
		{"", "ada@northwind.io", "https://northwind.io"},
		{"", "ada@Gmail.com", ""},
		{"", "ada@outlook.com", ""},
		{"", "ada@mailinator.com", ""},
		{"", "", ""},
		{"not a url", "ada@icloud.com", ""},
	} {
		if got := defaultForward(c.website, c.email); got != c.want {
			t.Errorf("defaultForward(%q, %q) = %q, want %q", c.website, c.email, got, c.want)
		}
	}
}

type brandSite string

func (b brandSite) GetWorkspaceBrand(context.Context, uuid.UUID) (*models.BrandSettings, error) {
	if b == "" {
		return nil, nil
	}
	return &models.BrandSettings{WebsiteURL: string(b)}, nil
}

type people struct{ email string }

func (p people) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	return &models.User{ID: id, Email: p.email}, nil
}

func (p people) GetOrganization(_ context.Context, id uuid.UUID) (*models.Organization, error) {
	return &models.Organization{ID: id}, nil
}

func TestDefaultForwardPrefersTheWorkspaceWebsite(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.s.People = people{email: "ada@northwind.io"}
	if got := h.s.DefaultForward(ctx, h.org, h.user); got != "https://northwind.io" {
		t.Fatalf("business email = %q", got)
	}
	h.s.Websites = brandSite("acme.com")
	if got := h.s.DefaultForward(ctx, h.org, h.user); got != "https://acme.com" {
		t.Fatalf("workspace website = %q", got)
	}
	h.s.Websites, h.s.People = brandSite(""), people{email: "ada@gmail.com"}
	if got := h.s.DefaultForward(ctx, h.org, h.user); got != "" {
		t.Fatalf("free provider = %q, want none", got)
	}
}

func TestLaterMailboxJoinsTheDomainTrackingHost(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	track := &trackingStub{}
	boxes := &memMailboxes{status: map[uuid.UUID]string{}}
	h.s.Mailboxes = boxes
	h.s.WireSendingDomains(track)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	if len(track.calls) != 1 {
		t.Fatalf("tracking setups = %v", track.calls)
	}
	host := config.DefaultTrackingHost("acmeoutreach.com")

	more := QuoteRequest{Items: []graphkit.QuoteItem{{Type: graphkit.ItemMailboxes, Domain: "acmeoutreach.com", Provider: graphkit.ProviderGoogle,
		Mailboxes: []graphkit.NewMailbox{{LocalPart: "bob", FirstName: "Bob", LastName: "Lee"}}}}}
	_, o := h.buy(t, more)
	h.settle(t)
	o, _ = h.s.GetOrder(ctx, h.org, o.ID)
	if len(o.Mailboxes) != 1 || o.Mailboxes[0].EmailAccountID == nil {
		t.Fatalf("later mailbox = %+v", o.Mailboxes)
	}
	boxes.mu.Lock()
	got := boxes.tracking[*o.Mailboxes[0].EmailAccountID]
	boxes.mu.Unlock()
	if got != host {
		t.Fatalf("the later mailbox tracks through %q, want %q", got, host)
	}
	if len(track.calls) != 1 {
		t.Fatalf("the host was set up again: %v", track.calls)
	}
}
