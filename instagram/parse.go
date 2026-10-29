package instagram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Errors returned by parsing and fetching. Callers use them to log why a post
// fell back to a plain link.
var (
	// ErrNoMediaData means the page had no embedded post data at all. This is
	// what Instagram serves when it rate limits or soft-blocks a client, and
	// would also happen if the page layout changes.
	ErrNoMediaData = errors.New("instagram: page contains no embedded media data")
	// ErrLoginRequired means the post exists but is gated for logged-out users
	// (private account, age restriction, ...), or we were redirected to login.
	ErrLoginRequired = errors.New("instagram: login required")
	// ErrNotFound means the post doesn't exist or was deleted.
	ErrNotFound = errors.New("instagram: post not found")
	// ErrRateLimited means Instagram answered with HTTP 429.
	ErrRateLimited = errors.New("instagram: rate limited")
	// ErrNoMedia means the post was parsed but contained no downloadable media.
	ErrNoMedia = errors.New("instagram: post has no downloadable media")
)

// MediaType is the type of a single post item.
type MediaType int

const (
	Photo MediaType = iota
	Video
)

// MediaItem is one photo or video of a post. A carousel has several.
type MediaItem struct {
	Type   MediaType
	URL    string // Best quality media URL.
	Width  int
	Height int
}

// Post is the media and metadata extracted from a post page.
type Post struct {
	Shortcode string
	Username  string
	FullName  string
	Caption   string
	Items     []MediaItem
}

// Instagram media_type values.
const (
	mediaTypeImage    = 1
	mediaTypeVideo    = 2
	mediaTypeCarousel = 8
)

type imageCandidate struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type videoVersion struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   int    `json:"type"`
}

// productMedia mirrors the subset of Instagram's "XIGPolarisMedia" / API v1
// media object that we need. Carousel children share the same shape.
type productMedia struct {
	Code           string `json:"code"`
	MediaType      int    `json:"media_type"`
	OriginalWidth  int    `json:"original_width"`
	OriginalHeight int    `json:"original_height"`
	ImageVersions2 *struct {
		Candidates []imageCandidate `json:"candidates"`
	} `json:"image_versions2"`
	VideoVersions []videoVersion `json:"video_versions"`
	CarouselMedia []productMedia `json:"carousel_media"`
	User          *struct {
		Username string `json:"username"`
		FullName string `json:"full_name"`
	} `json:"user"`
	Caption *struct {
		Text string `json:"text"`
	} `json:"caption"`
}

type polarisMedia struct {
	Code                string        `json:"code"`
	IfNotGatedLoggedOut *productMedia `json:"if_not_gated_logged_out"`
	GatingRuling        *struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"gating_ruling"`
}

// sjsScriptRe matches the server-side JSON payloads ("data-sjs" scripts) that
// Instagram embeds into its post pages.
var sjsScriptRe = regexp.MustCompile(`(?s)<script\b[^>]*\bdata-sjs\b[^>]*>(\{.+?\})</script>`)

var xigKey = []byte(`"xig_polaris_media"`)

// ParsePostPage extracts post media from the HTML of
// https://www.instagram.com/p/<shortcode>/ as served to a logged-out browser.
//
// The data lives in a Relay prefetch cache inside one of the data-sjs scripts:
// require[..].__bbox.require[RelayPrefetchedStreamCache][..].__bbox.result.data.xig_polaris_media.
// Instead of following that exact (and frequently changing) path we walk the
// decoded JSON for every "xig_polaris_media" object and prefer the one whose
// code matches shortcode (pass "" to accept the first one found).
func ParsePostPage(html []byte, shortcode string) (*Post, error) {
	var found []polarisMedia
	for _, m := range sjsScriptRe.FindAllSubmatch(html, -1) {
		if !bytes.Contains(m[1], xigKey) {
			continue
		}
		var doc any
		if err := json.Unmarshal(m[1], &doc); err != nil {
			continue
		}
		for _, raw := range findKey(doc, "xig_polaris_media", nil) {
			// Re-encode the subtree and decode it into typed structs.
			b, err := json.Marshal(raw)
			if err != nil {
				continue
			}
			var pm polarisMedia
			if err := json.Unmarshal(b, &pm); err != nil {
				return nil, fmt.Errorf("instagram: decoding xig_polaris_media: %w", err)
			}
			found = append(found, pm)
		}
	}
	if len(found) == 0 {
		return nil, ErrNoMediaData
	}
	media := &found[0]
	for i := range found {
		if shortcode != "" && found[i].Code == shortcode {
			media = &found[i]
			break
		}
	}
	if media.IfNotGatedLoggedOut == nil {
		if g := media.GatingRuling; g != nil && g.Title != "" {
			return nil, fmt.Errorf("%w (%s)", ErrLoginRequired, g.Title)
		}
		return nil, ErrLoginRequired
	}
	return postFromProduct(media.IfNotGatedLoggedOut)
}

func postFromProduct(p *productMedia) (*Post, error) {
	post := &Post{Shortcode: p.Code}
	if p.User != nil {
		post.Username = p.User.Username
		post.FullName = p.User.FullName
	}
	if p.Caption != nil {
		post.Caption = p.Caption.Text
	}

	nodes := p.CarouselMedia
	if p.MediaType != mediaTypeCarousel || len(nodes) == 0 {
		nodes = []productMedia{*p}
	}
	for i := range nodes {
		item, ok := itemFromProduct(&nodes[i])
		if !ok {
			return nil, fmt.Errorf("%w (item %d of %d)", ErrNoMedia, i+1, len(nodes))
		}
		post.Items = append(post.Items, item)
	}
	if len(post.Items) == 0 {
		return nil, ErrNoMedia
	}
	return post, nil
}

func itemFromProduct(p *productMedia) (MediaItem, bool) {
	if p.MediaType == mediaTypeVideo || len(p.VideoVersions) > 0 {
		if v, ok := bestVideo(p.VideoVersions); ok {
			w, h := v.Width, v.Height
			if w == 0 || h == 0 {
				w, h = p.OriginalWidth, p.OriginalHeight
			}
			return MediaItem{Type: Video, URL: v.URL, Width: w, Height: h}, true
		}
		if p.MediaType == mediaTypeVideo {
			return MediaItem{}, false
		}
	}
	if p.ImageVersions2 == nil {
		return MediaItem{}, false
	}
	c, ok := bestImage(p.ImageVersions2.Candidates)
	if !ok {
		return MediaItem{}, false
	}
	w, h := c.Width, c.Height
	if w == 0 || h == 0 {
		w, h = p.OriginalWidth, p.OriginalHeight
	}
	return MediaItem{Type: Photo, URL: c.URL, Width: w, Height: h}, true
}

// bestImage returns the candidate with the most pixels. Logged-out responses
// often omit dimensions; Instagram lists the full-size, uncropped rendition
// first, so ties keep the earlier candidate.
func bestImage(cands []imageCandidate) (imageCandidate, bool) {
	var best imageCandidate
	for _, c := range cands {
		if c.URL == "" {
			continue
		}
		if best.URL == "" || c.Width*c.Height > best.Width*best.Height {
			best = c
		}
	}
	return best, best.URL != ""
}

// bestVideo returns the largest video version. Logged-out responses often omit
// dimensions; Instagram lists the best version first, so ties keep the earlier one.
func bestVideo(vers []videoVersion) (videoVersion, bool) {
	var best videoVersion
	for _, v := range vers {
		if v.URL == "" {
			continue
		}
		if best.URL == "" || v.Width*v.Height > best.Width*best.Height {
			best = v
		}
	}
	return best, best.URL != ""
}

// findKey collects every non-null value stored under key, depth first.
// Values found under key are not searched further.
func findKey(v any, key string, out []any) []any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if k == key {
				if child != nil {
					out = append(out, child)
				}
				continue
			}
			out = findKey(child, key, out)
		}
	case []any:
		for _, child := range t {
			out = findKey(child, key, out)
		}
	}
	return out
}
