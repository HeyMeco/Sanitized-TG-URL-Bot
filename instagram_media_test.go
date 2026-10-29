package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"sanitizetelebot/instagram"
)

// withInstagramTestServer serves the instagram package's fixtures as post
// pages, with CDN URLs rewritten to point at the test server, and runs the
// test inside a temporary directory so cache files don't leak.
func withInstagramTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	pages := map[string]string{
		"DfAWBERlz3A": "post_single_image.html",
		"DeAYvsnn4la": "post_photo_carousel.html",
		"De-T1-ZiKeQ": "post_mixed_carousel.html",
		"Chunk8-jurw": "post_reel.html",
		"missing":     "post_missing.html",
		"gated":       "post_gated_synthetic.html",
	}
	fixtures := map[string][]byte{}
	for code, name := range pages {
		b, err := os.ReadFile("instagram/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		fixtures[code] = b
	}

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/p/", func(w http.ResponseWriter, r *http.Request) {
		code := strings.Trim(strings.TrimPrefix(r.URL.Path, "/p/"), "/")
		page, ok := fixtures[code]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(strings.ReplaceAll(string(page), "https://scontent-dus1-1.cdninstagram.com", srv.URL+"/cdn")))
	})
	mux.HandleFunc("/cdn/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".mp4") {
			w.Write([]byte("fake mp4 data"))
			return
		}
		w.Write([]byte("fake jpg data"))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldClient, oldFetch, oldFallback := igClient, instagramLocalFetch, instagramFallbackHost
	igClient = &instagram.Client{HTTP: srv.Client(), BaseURL: srv.URL}
	instagramLocalFetch, instagramFallbackHost = true, ""
	t.Cleanup(func() { igClient, instagramLocalFetch, instagramFallbackHost = oldClient, oldFetch, oldFallback })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	return srv
}

func TestSanitizeURLInstagramMedia(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		wantText  string
		wantKinds string // p = photo, v = video
		wantNote  string
	}{
		{
			name:      "single image",
			message:   "look https://www.instagram.com/p/DfAWBERlz3A/?igsh=MTc4MmM1YmI2Ng==",
			wantText:  "look https://www.instagram.com/p/DfAWBERlz3A/",
			wantKinds: "p",
			wantNote:  "Instagram post by nasa: Wow, so edgy",
		},
		{
			name:      "photo carousel",
			message:   "https://www.instagram.com/p/DeAYvsnn4la/?img_index=1",
			wantText:  "https://www.instagram.com/p/DeAYvsnn4la/?img_index=1",
			wantKinds: "ppp",
			wantNote:  "Instagram post by nasa: Swipe for countless stars",
		},
		{
			name:      "mixed carousel",
			message:   "https://instagram.com/natgeo/p/De-T1-ZiKeQ/",
			wantText:  "https://instagram.com/natgeo/p/De-T1-ZiKeQ/",
			wantKinds: "pvvpv",
			wantNote:  "Instagram post by natgeo: Over the summer",
		},
		{
			name:      "unknown tracking param",
			message:   "https://www.instagram.com/p/DeAYvsnn4la/?xtok=abc123",
			wantText:  "https://www.instagram.com/p/DeAYvsnn4la/",
			wantKinds: "ppp",
			wantNote:  "Instagram post by nasa: Swipe for countless stars",
		},
		{
			name:      "reel",
			message:   "https://www.instagram.com/reel/Chunk8-jurw/?igsh=abc&utm_source=ig_web_copy_link",
			wantText:  "https://www.instagram.com/reel/Chunk8-jurw/",
			wantKinds: "v",
			wantNote:  "Instagram post by instagram: “Gingerton”",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withInstagramTestServer(t)
			res, err := sanitizeURL(tt.message, true)
			if err != nil {
				t.Fatal(err)
			}
			defer removeMediaFiles(res.Media)

			if !res.WasSanitized || res.Text != tt.wantText {
				t.Errorf("got text=%q sanitized=%v, want %q", res.Text, res.WasSanitized, tt.wantText)
			}
			var kinds strings.Builder
			for _, m := range res.Media {
				if m.IsVideo {
					kinds.WriteByte('v')
				} else {
					kinds.WriteByte('p')
				}
				if _, err := os.Stat(m.Path); err != nil {
					t.Errorf("media file missing: %v", err)
				}
				wantDir := imageCacheDir
				if m.IsVideo {
					wantDir = videoCacheDir
				}
				if !strings.HasPrefix(m.Path, wantDir+string(os.PathSeparator)) {
					t.Errorf("media %s not in %s", m.Path, wantDir)
				}
			}
			if kinds.String() != tt.wantKinds {
				t.Errorf("media kinds = %q, want %q", kinds.String(), tt.wantKinds)
			}
			if len(res.MediaNotes) != 1 || !strings.HasPrefix(res.MediaNotes[0], tt.wantNote) {
				t.Errorf("notes = %q, want prefix %q", res.MediaNotes, tt.wantNote)
			}
		})
	}
}

func TestSanitizeURLInstagramFallback(t *testing.T) {
	withInstagramTestServer(t)

	// Parsing fails (no data / gated): the cleaned link is sent, no media.
	for _, code := range []string{"missing", "gated", "unknown"} {
		res, err := sanitizeURL("https://www.instagram.com/p/"+code+"/?igsh=abc", true)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Media) != 0 || res.Text != "https://www.instagram.com/p/"+code+"/" || !res.WasSanitized {
			t.Errorf("%s: got %+v", code, res)
		}
	}

	// Already clean link + failed parsing: nothing to do.
	res, _ := sanitizeURL("https://www.instagram.com/p/missing/", true)
	if res.WasSanitized {
		t.Errorf("clean link with failed parsing should not be reposted: %+v", res)
	}

	// With a fallback host configured, failed parsing rewrites the host.
	instagramFallbackHost = "eeinstagram.com"
	res, _ = sanitizeURL("https://www.instagram.com/reel/missing/", true)
	if res.Text != "https://eeinstagram.com/reel/missing/" || !res.WasSanitized || len(res.Media) != 0 {
		t.Errorf("fallback host: got %+v", res)
	}
	// ...but successful parsing still sends media with the instagram.com link.
	res, _ = sanitizeURL("https://www.instagram.com/reel/Chunk8-jurw/", true)
	defer removeMediaFiles(res.Media)
	if res.Text != "https://www.instagram.com/reel/Chunk8-jurw/" || len(res.Media) != 1 {
		t.Errorf("fallback host with success: got %+v", res)
	}
}

func TestSanitizeURLInstagramWithoutFetching(t *testing.T) {
	withInstagramTestServer(t)

	tests := []struct {
		in, want  string
		sanitized bool
	}{
		// Inline mode (fetchMedia=false): only parameter cleaning.
		{"https://www.instagram.com/p/DfAWBERlz3A/?igshid=abc", "https://www.instagram.com/p/DfAWBERlz3A/", true},
		{"https://www.instagram.com/reel/Chunk8-jurw/", "https://www.instagram.com/reel/Chunk8-jurw/", false},
		// Unknown tracking params (xtok) are dropped from post links; img_index is kept.
		{"https://www.instagram.com/p/DeAYvsnn4la/?img_index=3&xtok=abc#frag", "https://www.instagram.com/p/DeAYvsnn4la/?img_index=3", true},
		{"https://www.instagram.com/reel/Chunk8-jurw/?foo=bar", "https://www.instagram.com/reel/Chunk8-jurw/", true},
		// Non-post links (stories, profiles) are cleaned the same way.
		{"https://www.instagram.com/stories/natgeo/1234567890?utm_source=ig_story_item_share&srtk=abc", "https://www.instagram.com/stories/natgeo/1234567890", true},
		{"https://www.instagram.com/natgeo/?hl=de&srtk=abc", "https://www.instagram.com/natgeo/", true},
		{"https://www.instagram.com/explore/search/keyword/?q=wolves", "https://www.instagram.com/explore/search/keyword/?q=wolves", false},
		// Profile cards are still turned into profile links.
		{"https://www.instagram.com/natgeo/profilecard/?igsh=abc", "https://www.instagram.com/natgeo", true},
		// Proxy hosts are left alone.
		{"https://eeinstagram.com/p/DfAWBERlz3A/", "https://eeinstagram.com/p/DfAWBERlz3A/", false},
	}
	for _, tt := range tests {
		res, err := sanitizeURL(tt.in, false)
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != tt.want || res.WasSanitized != tt.sanitized || len(res.Media) != 0 {
			t.Errorf("sanitizeURL(%q) = %q sanitized=%v media=%d, want %q sanitized=%v", tt.in, res.Text, res.WasSanitized, len(res.Media), tt.want, tt.sanitized)
		}
	}

	// Local fetching can be disabled entirely.
	instagramLocalFetch = false
	res, _ := sanitizeURL("https://www.instagram.com/reel/Chunk8-jurw/?igsh=x", true)
	if len(res.Media) != 0 || res.Text != "https://www.instagram.com/reel/Chunk8-jurw/" {
		t.Errorf("INSTAGRAM_LOCAL_FETCH=false: got %+v", res)
	}
}

func TestBuildMediaCaption(t *testing.T) {
	if got := buildMediaCaption("@bob said: a<b & c", nil); got != "@bob said: a&lt;b &amp; c" {
		t.Errorf("no notes: %q", got)
	}
	got := buildMediaCaption("@bob said: link", []string{"Instagram post by nasa: <3"})
	if want := "@bob said: link\n<blockquote expandable>Instagram post by nasa: &lt;3</blockquote>"; got != want {
		t.Errorf("with note: %q, want %q", got, want)
	}

	// Long notes are cut so the visible text fits Telegram's limit.
	long := strings.Repeat("ü", 2000)
	got = buildMediaCaption("@bob said: link", []string{long})
	visible := strings.NewReplacer("<blockquote expandable>", "", "</blockquote>", "").Replace(got)
	if n := len([]rune(visible)); n != maxCaptionRunes {
		t.Errorf("caption has %d visible runes, want %d", n, maxCaptionRunes)
	}
	if !strings.HasPrefix(got, "@bob said: link\n<blockquote expandable>") || !strings.HasSuffix(got, "…</blockquote>") {
		t.Errorf("long note caption = %q...", got[:60])
	}

	// A huge message leaves no room for notes; the base itself is shortened.
	got = buildMediaCaption(strings.Repeat("a", 1500), []string{"note"})
	if n := len([]rune(got)); n != maxCaptionRunes || strings.Contains(got, "note") {
		t.Errorf("long base: %d runes", n)
	}
}

func TestInstagramNote(t *testing.T) {
	post := &instagram.Post{Username: "natgeo", Caption: "  Wolves\u2063\n\u2063\n#InTheMoment  "}
	if got := instagramNote(post); got != "Instagram post by natgeo: Wolves\n\n#InTheMoment" {
		t.Errorf("note = %q", got)
	}
	if got := instagramNote(&instagram.Post{Username: "nasa"}); got != "Instagram post by nasa" {
		t.Errorf("no caption: %q", got)
	}
	long := instagramNote(&instagram.Post{Username: "x", Caption: strings.Repeat("y", 1000)})
	if n := len([]rune(long)); n != len("Instagram post by x: ")+maxNoteCaptionRunes {
		t.Errorf("long caption note has %d runes", n)
	}
}

func TestMediaFileInputMedia(t *testing.T) {
	if _, ok := (mediaFile{Path: "a.mp4", IsVideo: true}).inputMedia("c").(interface{ MediaType() string }); !ok {
		t.Fatal("inputMedia should return a media type")
	}
	v := (mediaFile{Path: "a.mp4", IsVideo: true, Width: 720, Height: 1280}).inputMedia("cap")
	if v.InputMedia().Type != "video" || v.InputMedia().Width != 720 || v.InputMedia().Caption != "cap" {
		t.Errorf("video input media = %+v", v.InputMedia())
	}
	p := (mediaFile{Path: "a.jpg"}).inputMedia("")
	if p.InputMedia().Type != "photo" {
		t.Errorf("photo input media = %+v", p.InputMedia())
	}
}

func TestDownloadMediaSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	dir := t.TempDir()

	if _, err := downloadMedia(srv.URL+"/big.mp4", mediaDownload{dir: dir, defaultExt: ".mp4", client: srv.Client(), maxBytes: 50}); err == nil {
		t.Error("expected size limit error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("oversized download left %d files behind", len(entries))
	}
	path, err := downloadMedia(srv.URL+"/ok.mp4", mediaDownload{dir: dir, defaultExt: ".mp4", client: srv.Client(), maxBytes: 100})
	if err != nil || !strings.HasSuffix(path, ".mp4") {
		t.Errorf("downloadMedia = %q, %v", path, err)
	}
}

func TestTikTokNote(t *testing.T) {
	var resp TikwmResponse
	body := `{"code":0,"msg":"success","data":{"title":" Sunset dump 🌅 #travel ","author":{"unique_id":"some.user","nickname":"Some User"},"images":["https://cdn.example/1.jpg","https://cdn.example/2.jpg"]}}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if got := tikTokNote(resp.Data); got != "TikTok post by some.user: Sunset dump 🌅 #travel" {
		t.Errorf("note = %q", got)
	}
	if got := tikTokNote(Data{}); got != "TikTok post" {
		t.Errorf("empty note = %q", got)
	}
}
