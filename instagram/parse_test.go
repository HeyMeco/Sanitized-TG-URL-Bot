package instagram

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// Fixtures in testdata/ are real logged-out post pages saved from
// www.instagram.com (October 2026), pruned to the data-sjs script that holds
// xig_polaris_media. Signed CDN query strings were stripped except for "stp",
// which encodes each rendition's size. post_gated_synthetic.html is derived
// from post_single_image.html with if_not_gated_logged_out set to null.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type wantItem struct {
	typ    MediaType
	width  int
	height int
	urlHas string
}

func TestParsePostPage(t *testing.T) {
	tests := []struct {
		fixture       string
		shortcode     string
		username      string
		fullName      string
		captionPrefix string
		items         []wantItem
	}{
		{
			fixture:       "post_single_image.html",
			shortcode:     "DfAWBERlz3A",
			username:      "nasa",
			fullName:      "NASA",
			captionPrefix: "Wow, so edgy",
			items: []wantItem{
				// First (uncropped, full size) candidate; dims come from original_width/height.
				{Photo, 1439, 1396, "2543695975749519193_n.jpg?stp=dst-jpg_e35_tt6&"},
			},
		},
		{
			fixture:       "post_photo_carousel.html",
			shortcode:     "DeAYvsnn4la",
			username:      "nasa",
			fullName:      "NASA",
			captionPrefix: "Swipe for countless stars",
			items: []wantItem{
				{Photo, 1080, 1350, "831222439_"},
				{Photo, 1080, 1350, "830418338_"},
				{Photo, 1080, 1350, "831354642_"},
			},
		},
		{
			fixture:       "post_mixed_carousel.html",
			shortcode:     "De-T1-ZiKeQ",
			username:      "natgeo",
			fullName:      "National Geographic",
			captionPrefix: "Over the summer, Vicky Ossio",
			items: []wantItem{
				{Photo, 1080, 1350, "868670377_"},
				{Video, 1080, 1350, ".mp4"},
				{Video, 1080, 1350, ".mp4"},
				{Photo, 1080, 1350, "866715771_"},
				{Video, 1080, 1350, ".mp4"},
			},
		},
		{
			fixture:       "post_reel.html",
			shortcode:     "Chunk8-jurw",
			username:      "instagram",
			fullName:      "Instagram",
			captionPrefix: "“Gingerton”",
			items: []wantItem{
				{Video, 720, 1280, ".mp4"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			post, err := ParsePostPage(readFixture(t, tt.fixture), tt.shortcode)
			if err != nil {
				t.Fatalf("ParsePostPage: %v", err)
			}
			if post.Shortcode != tt.shortcode || post.Username != tt.username || post.FullName != tt.fullName {
				t.Errorf("got shortcode=%q username=%q fullName=%q", post.Shortcode, post.Username, post.FullName)
			}
			if !strings.HasPrefix(post.Caption, tt.captionPrefix) {
				t.Errorf("caption = %q, want prefix %q", post.Caption, tt.captionPrefix)
			}
			if len(post.Items) != len(tt.items) {
				t.Fatalf("got %d items, want %d: %+v", len(post.Items), len(tt.items), post.Items)
			}
			for i, want := range tt.items {
				got := post.Items[i]
				if got.Type != want.typ || got.Width != want.width || got.Height != want.height {
					t.Errorf("item %d = type %d %dx%d, want type %d %dx%d", i, got.Type, got.Width, got.Height, want.typ, want.width, want.height)
				}
				if !strings.HasPrefix(got.URL, "https://") || !strings.Contains(got.URL, want.urlHas) {
					t.Errorf("item %d URL = %q, want it to contain %q", i, got.URL, want.urlHas)
				}
			}
		})
	}
}

func TestParsePostPageWithoutShortcodeHint(t *testing.T) {
	post, err := ParsePostPage(readFixture(t, "post_reel.html"), "")
	if err != nil {
		t.Fatal(err)
	}
	if post.Shortcode != "Chunk8-jurw" {
		t.Errorf("shortcode = %q", post.Shortcode)
	}
}

func TestParsePostPageErrors(t *testing.T) {
	tests := []struct {
		name string
		page []byte
		want error
	}{
		{"missing post page", readFixture(t, "post_missing.html"), ErrNoMediaData},
		{"gated post", readFixture(t, "post_gated_synthetic.html"), ErrLoginRequired},
		{"empty", nil, ErrNoMediaData},
		{"login page html", []byte("<html><body><form id=loginForm></form></body></html>"), ErrNoMediaData},
		{"broken json", []byte(`<script type="application/json" data-sjs>{"xig_polaris_media": </script>`), ErrNoMediaData},
		{
			"video without versions",
			[]byte(`<script type="application/json" data-sjs>{"a":{"xig_polaris_media":{"code":"X","if_not_gated_logged_out":{"code":"X","media_type":2}}}}</script>`),
			ErrNoMedia,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePostPage(tt.page, "X")
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParsePostPagePrefersMatchingShortcode(t *testing.T) {
	page := []byte(`<script type="application/json" data-sjs>{"require":[` +
		`{"xig_polaris_media":{"code":"OTHER","if_not_gated_logged_out":{"code":"OTHER","media_type":1,"image_versions2":{"candidates":[{"url":"https://cdn.example/other.jpg"}]}}}},` +
		`{"xig_polaris_media":{"code":"WANTED","if_not_gated_logged_out":{"code":"WANTED","media_type":1,"image_versions2":{"candidates":[{"url":"https://cdn.example/wanted.jpg"}]}}}}` +
		`]}</script>`)
	post, err := ParsePostPage(page, "WANTED")
	if err != nil {
		t.Fatal(err)
	}
	if post.Items[0].URL != "https://cdn.example/wanted.jpg" {
		t.Errorf("picked %q", post.Items[0].URL)
	}
}

func TestBestImageAndVideo(t *testing.T) {
	img, ok := bestImage([]imageCandidate{
		{URL: "https://a/small.jpg", Width: 320, Height: 320},
		{URL: "https://a/big.jpg", Width: 1080, Height: 1350},
		{URL: "", Width: 4000, Height: 4000},
	})
	if !ok || img.URL != "https://a/big.jpg" {
		t.Errorf("bestImage = %+v, %v", img, ok)
	}
	if _, ok := bestImage(nil); ok {
		t.Error("bestImage(nil) should fail")
	}
	vid, ok := bestVideo([]videoVersion{
		{URL: "https://a/first.mp4", Type: 101},
		{URL: "https://a/second.mp4", Type: 102},
	})
	if !ok || vid.URL != "https://a/first.mp4" {
		t.Errorf("bestVideo without dims = %+v, want first", vid)
	}
	vid, _ = bestVideo([]videoVersion{
		{URL: "https://a/480.mp4", Width: 480, Height: 852},
		{URL: "https://a/720.mp4", Width: 720, Height: 1280},
	})
	if vid.URL != "https://a/720.mp4" {
		t.Errorf("bestVideo with dims = %+v, want 720", vid)
	}
}
