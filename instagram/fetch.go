package instagram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// UserAgent is a desktop Chrome UA. Instagram only server-renders post data
// for requests that look like a top-level browser navigation, so it is sent
// together with the headers in SetBrowserHeaders.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

// maxPageBytes caps how much of a post page we read (pages are ~0.5-1 MB).
const maxPageBytes = 8 << 20

// Client fetches public post data from instagram.com.
type Client struct {
	HTTP *http.Client
	// BaseURL is overridable for tests. Defaults to https://www.instagram.com.
	BaseURL string
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return "https://www.instagram.com"
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// SetBrowserHeaders makes req look like a top-level Chrome navigation.
func SetBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Ch-Ua", `"Chromium";v="130", "Google Chrome";v="130", "Not?A_Brand";v="99"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)
}

// isLoginRedirect reports whether Instagram bounced us to the login page or
// the home page, which it does for gated posts and when rate limiting.
func isLoginRedirect(u *url.URL) bool {
	return strings.HasPrefix(u.Path, "/accounts/login") || u.Path == "/" || u.Path == ""
}

// FetchPost downloads the public post page for shortcode and parses its media.
func (c *Client) FetchPost(ctx context.Context, shortcode string) (*Post, error) {
	if !shortcodeRe.MatchString(shortcode) {
		return nil, fmt.Errorf("instagram: invalid shortcode %q", shortcode)
	}
	pageURL := c.baseURL() + "/p/" + shortcode + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	SetBrowserHeaders(req)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("instagram: fetching %s: %w", pageURL, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("instagram: fetching %s: unexpected status %s", pageURL, resp.Status)
	case isLoginRedirect(resp.Request.URL):
		return nil, fmt.Errorf("%w (redirected to %s)", ErrLoginRequired, resp.Request.URL.Path)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("instagram: reading %s: %w", pageURL, err)
	}
	post, err := ParsePostPage(body, shortcode)
	if err != nil {
		return nil, err
	}
	if post.Shortcode == "" {
		post.Shortcode = shortcode
	}
	return post, nil
}

var errStopRedirect = errors.New("stop redirect")

// ResolveShare follows an /share/... link until it reaches a post URL and
// returns that post link. Share links normally answer with an HTTP redirect;
// if they don't, the og:url / canonical link of the returned page is used.
func (c *Client) ResolveShare(ctx context.Context, shareURL string) (Link, error) {
	var found Link
	hc := *c.httpClient()
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if l := linkFromRedirect(req.URL); l.Kind == KindPost {
			found = l
			return errStopRedirect
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shareURL, nil)
	if err != nil {
		return Link{}, err
	}
	SetBrowserHeaders(req)
	resp, err := hc.Do(req)
	if found.Kind == KindPost {
		if resp != nil {
			resp.Body.Close()
		}
		return found, nil
	}
	if err != nil {
		return Link{}, fmt.Errorf("instagram: resolving share link: %w", err)
	}
	defer resp.Body.Close()
	if l := linkFromRedirect(resp.Request.URL); l.Kind == KindPost {
		return l, nil
	}
	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
		if l := LinkFromPageMeta(body); l.Kind == KindPost {
			return l, nil
		}
	}
	return Link{}, fmt.Errorf("instagram: share link did not redirect to a post (ended at %s, status %s)", resp.Request.URL.Redacted(), resp.Status)
}

var (
	ogURLRe     = regexp.MustCompile(`<meta[^>]+property="og:url"[^>]+content="([^"]+)"`)
	canonicalRe = regexp.MustCompile(`<link[^>]+rel="canonical"[^>]+href="([^"]+)"`)
)

// LinkFromPageMeta returns the post link advertised by a page's og:url or
// canonical link, if any.
func LinkFromPageMeta(page []byte) Link {
	for _, re := range []*regexp.Regexp{ogURLRe, canonicalRe} {
		m := re.FindSubmatch(page)
		if m == nil {
			continue
		}
		u, err := url.Parse(html.UnescapeString(string(m[1])))
		if err != nil {
			continue
		}
		if l := ParseLink(u); l.Kind == KindPost {
			return l
		}
	}
	return Link{}
}
