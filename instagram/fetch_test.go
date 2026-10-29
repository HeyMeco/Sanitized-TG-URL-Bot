package instagram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFetchPost(t *testing.T) {
	reel := readFixture(t, "post_reel.html")
	missing := readFixture(t, "post_missing.html")

	mux := http.NewServeMux()
	mux.HandleFunc("/p/Chunk8-jurw/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Fetch-Mode") != "navigate" || r.Header.Get("User-Agent") != UserAgent {
			// Mimic Instagram: without browser navigation headers the page has no data.
			w.Write(missing)
			return
		}
		w.Write(reel)
	})
	mux.HandleFunc("/p/missing/", func(w http.ResponseWriter, r *http.Request) { w.Write(missing) })
	mux.HandleFunc("/p/gone/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/p/limited/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/p/walled/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/accounts/login/?next=/p/walled/", http.StatusFound)
	})
	mux.HandleFunc("/accounts/login/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>login</html>")) })
	mux.HandleFunc("/p/broken/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	post, err := c.FetchPost(ctx, "Chunk8-jurw")
	if err != nil {
		t.Fatalf("FetchPost: %v", err)
	}
	if post.Username != "instagram" || len(post.Items) != 1 || post.Items[0].Type != Video {
		t.Errorf("unexpected post %+v", post)
	}

	for code, want := range map[string]error{
		"missing": ErrNoMediaData,
		"gone":    ErrNotFound,
		"limited": ErrRateLimited,
		"walled":  ErrLoginRequired,
	} {
		if _, err := c.FetchPost(ctx, code); !errors.Is(err, want) {
			t.Errorf("FetchPost(%s) err = %v, want %v", code, err, want)
		}
	}
	if _, err := c.FetchPost(ctx, "broken"); err == nil {
		t.Error("FetchPost(broken) should fail")
	}
	if _, err := c.FetchPost(ctx, "../etc"); err == nil {
		t.Error("FetchPost with invalid shortcode should fail")
	}
}

func TestResolveShare(t *testing.T) {
	var targetHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/share/reel/redirect/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/reel/DehZj9wggt0/?igsh=MWQ1ZGUxMzBkMA==", http.StatusFound)
	})
	mux.HandleFunc("/share/chain/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/share/reel/redirect/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/share/login/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/accounts/login/?next=%2Fp%2FDfAWBERlz3A%2F", http.StatusFound)
	})
	mux.HandleFunc("/share/meta/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:url" content="https://www.instagram.com/p/De-T1-ZiKeQ/" /></head></html>`))
	})
	mux.HandleFunc("/share/dead/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>Instagram</title></head></html>`))
	})
	mux.HandleFunc("/reel/", func(w http.ResponseWriter, r *http.Request) { targetHits++ })
	mux.HandleFunc("/p/", func(w http.ResponseWriter, r *http.Request) { targetHits++ })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The test server's host isn't instagram.com, so redirects to it aren't
	// recognised as posts. Rewrite redirect targets to instagram.com in the
	// transport while still routing every request to the test server.
	hc := srv.Client()
	hc.Transport = rewriteTransport{target: srv.URL, base: hc.Transport}
	c := &Client{HTTP: hc}
	ctx := context.Background()

	for path, want := range map[string]string{
		"/share/reel/redirect/": "DehZj9wggt0",
		"/share/chain/":         "DehZj9wggt0",
		"/share/login/":         "DfAWBERlz3A",
		"/share/meta/":          "De-T1-ZiKeQ",
	} {
		l, err := c.ResolveShare(ctx, "https://www.instagram.com"+path)
		if err != nil || l.Shortcode != want {
			t.Errorf("ResolveShare(%s) = %+v, %v; want %s", path, l, err, want)
		}
	}
	if _, err := c.ResolveShare(ctx, "https://www.instagram.com/share/dead/"); err == nil {
		t.Error("ResolveShare(dead) should fail")
	}
	if targetHits != 0 {
		t.Errorf("ResolveShare downloaded the target post page %d times", targetHits)
	}
}

// rewriteTransport sends every request to target while keeping the original
// URL (and therefore Location resolution) on www.instagram.com.
type rewriteTransport struct {
	target string
	base   http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	target, err := url.Parse(rt.target)
	if err != nil {
		return nil, err
	}
	u := *req.URL
	u.Scheme, u.Host = target.Scheme, target.Host
	r.URL = &u
	r.Host = ""
	resp, err := rt.base.RoundTrip(r)
	if resp != nil {
		resp.Request = req
	}
	return resp, err
}
