package handler

import (
	"strings"
	"testing"
)

func TestValidateTemplatePayload(t *testing.T) {
	cases := []struct {
		name    string
		payload warmupTemplatePayload
		wantErr bool
	}{
		{name: "plain copy passes", payload: warmupTemplatePayload{Subject: "Quick question", Body: "Hi Sam, saw your post. Still comparing tools?"}},
		{name: "markup in body passes verbatim", payload: warmupTemplatePayload{Subject: "Q", Body: "Hi {first_name},\nSee <https://example.com> for the plan."}},
		{name: "missing subject", payload: warmupTemplatePayload{Body: "body"}, wantErr: true},
		{name: "missing body", payload: warmupTemplatePayload{Subject: "subject"}, wantErr: true},
		{name: "subject too long", payload: warmupTemplatePayload{Subject: strings.Repeat("a", 201), Body: "body"}, wantErr: true},
		{name: "body too long", payload: warmupTemplatePayload{Subject: "s", Body: strings.Repeat("a", 51*1024)}, wantErr: true},
		{name: "markup in subject refused", payload: warmupTemplatePayload{Subject: "Hi <name>", Body: "body"}, wantErr: true},
		{name: "newline in subject refused", payload: warmupTemplatePayload{Subject: "a\nb", Body: "body"}, wantErr: true},
		{name: "NUL in body refused", payload: warmupTemplatePayload{Subject: "s", Body: "a\x00b"}, wantErr: true},
		{name: "warmup wording refused", payload: warmupTemplatePayload{Subject: "s", Body: "this is a warmup email test"}, wantErr: true},
		{name: "copy the send gate would block is refused", payload: warmupTemplatePayload{Subject: "Free cash bonus", Body: "Act now to claim your free cash bonus."}, wantErr: true},
		{name: "merge fields refused, since nothing renders them", payload: warmupTemplatePayload{Subject: "Hiring?", Body: "Hi {{.FirstName}}, are you hiring?"}, wantErr: true},
		{name: "AI blocks refused", payload: warmupTemplatePayload{Subject: "Hiring?", Body: "Saw [[ai:icebreaker]], are you hiring?"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTemplatePayload(&tc.payload)
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error for %+v", tc.payload)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
