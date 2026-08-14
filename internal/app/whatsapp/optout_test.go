package whatsapp

import "testing"

func TestDetectOptOut(t *testing.T) {
	cases := []struct {
		body      string
		matched   bool
		confident bool
	}{
		{"Não tenho interesse, obrigado", true, true},
		{"PARE de me enviar mensagens", true, true},
		{"parar", true, true},
		{"stop", true, true},
		{"Retire meu número da lista", true, true},
		{"Agora não, talvez depois", true, false},
		{"Pode me explicar o serviço?", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		m := DetectOptOut(tc.body)
		if m.Matched != tc.matched || m.Confident != tc.confident {
			t.Errorf("body=%q got matched=%v conf=%v cat=%s phrase=%q",
				tc.body, m.Matched, m.Confident, m.Category, m.Phrase)
		}
	}
}
