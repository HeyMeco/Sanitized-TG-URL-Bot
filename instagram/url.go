// Package instagram extracts media from public Instagram posts without
// relying on third-party proxy services.
package instagram

import (
	"net/url"
	"regexp"
	"strings"
)

// LinkKind classifies an Instagram URL.
type LinkKind int

const (
	// KindOther is any Instagram URL we don't fetch media for (profiles, stories, ...).
	KindOther LinkKind = iota
	// KindPost is a post, carousel, reel or IGTV link that carries a shortcode.
	KindPost
	// KindShare is an app share link (/share/...) that redirects to a post.
	KindShare
)

// Link is the result of classifying an Instagram URL.
type Link struct {
	Kind      LinkKind
	Shortcode string // Set for KindPost.
	IsReel    bool   // True for /reel/, /reels/ and /tv/ links.
}

var shortcodeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// IsInstagramHost reports whether host is instagram.com or one of its subdomains
// (www., m., ...). Proxy hosts like eeinstagram.com are deliberately excluded.
func IsInstagramHost(host string) bool {
	host = strings.ToLower(host)
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	return host == "instagram.com" || strings.HasSuffix(host, ".instagram.com")
}

// ParseLink classifies an Instagram URL. Supported post forms:
//
//	/p/<code>/  /reel/<code>/  /reels/<code>/  /tv/<code>/
//	/<username>/p/<code>/  /<username>/reel/<code>/
//
// and share links /share/<id>/, /share/p/<id>/, /share/reel/<id>/.
func ParseLink(u *url.URL) Link {
	if u == nil || !IsInstagramHost(u.Host) {
		return Link{}
	}
	segs := pathSegments(u.Path)
	if len(segs) == 0 {
		return Link{}
	}
	if segs[0] == "share" {
		if len(segs) >= 2 {
			return Link{Kind: KindShare}
		}
		return Link{}
	}

	// Allow one optional leading username segment, like yt-dlp does.
	for _, start := range []int{0, 1} {
		if len(segs) < start+2 {
			break
		}
		kind, code := segs[start], segs[start+1]
		if kind == "reels" && code == "audio" {
			return Link{}
		}
		switch kind {
		case "p", "reel", "reels", "tv":
			if shortcodeRe.MatchString(code) {
				return Link{Kind: KindPost, Shortcode: code, IsReel: kind != "p"}
			}
			return Link{}
		}
	}
	return Link{}
}

// PostURL returns the canonical public URL for a shortcode.
func PostURL(shortcode string, isReel bool) string {
	if isReel {
		return "https://www.instagram.com/reel/" + shortcode + "/"
	}
	return "https://www.instagram.com/p/" + shortcode + "/"
}

func pathSegments(p string) []string {
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// linkFromRedirect extracts a post link from a URL that a share link redirected
// to. Logged-out clients are sometimes sent to /accounts/login/?next=/reel/<code>/,
// so the "next" parameter is checked as well.
func linkFromRedirect(u *url.URL) Link {
	if l := ParseLink(u); l.Kind == KindPost {
		return l
	}
	if next := u.Query().Get("next"); next != "" {
		if nu, err := url.Parse(next); err == nil {
			if !nu.IsAbs() {
				nu = u.ResolveReference(nu)
			}
			if l := ParseLink(nu); l.Kind == KindPost {
				return l
			}
		}
	}
	return Link{}
}
