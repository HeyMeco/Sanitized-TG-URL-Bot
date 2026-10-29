package main

import (
	"os"
	"strings"
	"testing"
)

// TestInstagramLive runs the full fetch + download path against instagram.com.
// It is skipped unless IG_LIVE_TEST=1, so `go test ./...` never needs network.
// It never talks to Telegram.
//
//	IG_LIVE_TEST=1 go test -run TestInstagramLive -v .
func TestInstagramLive(t *testing.T) {
	if os.Getenv("IG_LIVE_TEST") != "1" {
		t.Skip("set IG_LIVE_TEST=1 to run against instagram.com")
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })

	tests := []struct {
		name, url, kinds string
	}{
		{"single image", "https://www.instagram.com/p/DfAWBERlz3A/", "p"},
		{"photo carousel", "https://www.instagram.com/p/DeAYvsnn4la/", "ppp"},
		{"mixed carousel", "https://www.instagram.com/p/De-T1-ZiKeQ/", "pvvpv"},
		{"video carousel", "https://www.instagram.com/p/BQ0eAlwhDrw/", "vvv"},
		{"reel", "https://www.instagram.com/reel/Chunk8-jurw/?igsh=test", "v"},
		{"reels path", "https://www.instagram.com/reels/DfAOoAXRg3L/", "v"},
		{"username reel path", "https://www.instagram.com/natgeo/reel/DehZj9wggt0/", "v"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := sanitizeURL(tt.url, true)
			if err != nil {
				t.Fatal(err)
			}
			defer removeMediaFiles(res.Media)

			var kinds strings.Builder
			for _, m := range res.Media {
				st, err := os.Stat(m.Path)
				if err != nil {
					t.Fatalf("missing file: %v", err)
				}
				kind := "photo"
				if m.IsVideo {
					kinds.WriteByte('v')
					kind = "video"
				} else {
					kinds.WriteByte('p')
				}
				t.Logf("%s %dx%d %d bytes", kind, m.Width, m.Height, st.Size())
			}
			t.Logf("text=%q notes=%q", res.Text, res.MediaNotes)
			if kinds.String() != tt.kinds {
				t.Errorf("media kinds = %q, want %q", kinds.String(), tt.kinds)
			}
		})
	}
}
