package llm

import "testing"

func TestAssertLocalEndpoint(t *testing.T) {
	cases := []struct {
		name       string
		url        string
		allowPub   bool
		wantErr    bool
		wantAllPriv bool
	}{
		{"loopback", "http://127.0.0.1:11434", false, false, true},
		{"localhost", "http://localhost:11434", false, false, true},
		{"private-10", "http://10.0.0.5:8000", false, false, true},
		{"private-192", "http://192.168.1.20:11434", false, false, true},
		{"public-blocked", "http://8.8.8.8:11434", false, true, false},
		{"public-allowed-override", "http://8.8.8.8:11434", true, false, false},
		{"invalid", "not-a-url", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := AssertLocalEndpoint(tc.url, tc.allowPub)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got none (assertion=%+v)", a)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && a.AllPrivate != tc.wantAllPriv {
				t.Fatalf("AllPrivate=%v, want %v", a.AllPrivate, tc.wantAllPriv)
			}
		})
	}
}
