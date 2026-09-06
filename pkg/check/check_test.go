package check

import "testing"

func TestParseTelegramState(t *testing.T) {
	cases := []struct {
		in    string
		layer int
		nonce string
		ok    bool
	}{
		{"notif-layer 229", 229, "", true},
		{"notif-layer 229 n=ab12cd34", 229, "ab12cd34", true},
		{"  notif-layer 214  ", 214, "", true},
		{"Pantau layer Telegram", 0, "", false},
		{"", 0, "", false},
	}
	for _, tc := range cases {
		state, ok := parseTelegramState(tc.in)
		if ok != tc.ok {
			t.Fatalf("parse %q ok=%v, want %v", tc.in, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if state.Layer != tc.layer || state.Nonce != tc.nonce {
			t.Fatalf("parse %q = %+v, want layer=%d nonce=%q", tc.in, state, tc.layer, tc.nonce)
		}
	}
}

func TestFormatTelegramState(t *testing.T) {
	if got := formatTelegramState(229, ""); got != "notif-layer 229" {
		t.Fatalf("got %q", got)
	}
	if got := formatTelegramState(229, "ab12cd34"); got != "notif-layer 229 n=ab12cd34" {
		t.Fatalf("got %q", got)
	}
}

func TestChangelogStopsAtKnownSHA(t *testing.T) {
	commits := []githubCommit{
		{SHA: "new1"},
		{SHA: "new2"},
		{SHA: "old1"},
	}
	commits[0].Commit.Message = "Update API scheme to layer 229"
	commits[1].Commit.Message = "Update API scheme to layer 228"
	commits[2].Commit.Message = "older change"

	got := changelog(commits, "old1", 214, 229)
	if len(got) != 2 {
		t.Fatalf("got %d changes: %v", len(got), got)
	}
	if got[0] != "Update API scheme to layer 229" || got[1] != "Update API scheme to layer 228" {
		t.Fatalf("unexpected changes: %v", got)
	}
}

func TestChangelogDedupsExactMessage(t *testing.T) {
	commits := []githubCommit{{SHA: "a"}, {SHA: "b"}}
	commits[0].Commit.Message = "Update API scheme to layer 229"
	commits[1].Commit.Message = "Update API scheme to layer 229"
	got := changelog(commits, "", 214, 229)
	if len(got) != 1 {
		t.Fatalf("got %d changes: %v", len(got), got)
	}
}
