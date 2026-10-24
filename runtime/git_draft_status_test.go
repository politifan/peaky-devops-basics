package main

import "testing"

func TestPorcelainHasUntrackedDraft(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"exact draft", "?? draft.txt\x00", true},
		{"backup only", "?? draft.txt.bak\x00", false},
		{"exact and backup", "?? draft.txt.bak\x00?? draft.txt\x00", true},
		{"tracked draft", " M draft.txt\x00", false},
		{"staged draft", "A  draft.txt\x00", false},
		{"directory prefix", "?? notes/draft.txt\x00", false},
		{"space suffix", "?? draft.txt \x00", false},
		{"space prefix", "??  draft.txt\x00", false},
		{"literal quoted filename", "?? \"draft.txt\"\x00", false},
		{"newline filename cannot inject record", "?? notes\n?? draft.txt\x00", false},
		{"special filename and exact draft", "?? \"notes\twith spaces\"\x00?? draft.txt\x00", true},
		{"rename source resembles status", "R  renamed.txt\x00?? draft.txt\x00", false},
		{"copy source resembles status", "C  copied.txt\x00?? draft.txt\x00", false},
		{"rename then exact untracked", "R  renamed.txt\x00old.txt\x00?? draft.txt\x00", true},
		{"missing rename source", "R  renamed.txt\x00", false},
		{"missing NUL terminator", "?? draft.txt", false},
		{"malformed after exact", "?? draft.txt\x00bad\x00", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := porcelainHasUntrackedPath([]byte(tc.output), "draft.txt"); got != tc.want {
				t.Fatalf("got %v, want %v for %q", got, tc.want, tc.output)
			}
		})
	}
}
