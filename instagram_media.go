package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"sanitizetelebot/instagram"
)

const (
	// Set INSTAGRAM_LOCAL_FETCH=false to skip local parsing and only clean links.
	instagramLocalFetchEnvVar = "INSTAGRAM_LOCAL_FETCH"
	// Set INSTAGRAM_FALLBACK_HOST (e.g. eeinstagram.com) to rewrite post links to
	// an embed proxy whenever local parsing is skipped or fails. Empty by default:
	// the cleaned instagram.com link is sent instead.
	instagramFallbackHostEnvVar = "INSTAGRAM_FALLBACK_HOST"

	instagramFetchTimeout = 30 * time.Second
)

var (
	igClient              = &instagram.Client{HTTP: httpClient}
	instagramLocalFetch   = envBool(instagramLocalFetchEnvVar, true)
	instagramFallbackHost = strings.TrimSpace(os.Getenv(instagramFallbackHostEnvVar))
)

func envBool(name string, def bool) bool {
	v, err := strconv.ParseBool(os.Getenv(name))
	if err != nil {
		return def
	}
	return v
}

// instagramResult describes what processInstagramURL did to a URL.
type instagramResult struct {
	changed bool
	url     *url.URL
	media   []mediaFile
	note    string
}

// processInstagramURL resolves share links, downloads post media when
// fetchMedia is set, and otherwise falls back to the cleaned link (or the
// configured fallback host). u is not modified.
func processInstagramURL(u *url.URL, fetchMedia bool) instagramResult {
	out := instagramResult{url: u}
	if !instagram.IsInstagramHost(u.Host) {
		return out
	}

	link := instagram.ParseLink(u)
	if link.Kind == instagram.KindShare {
		// Share links identify the sharer, so replace them with the canonical post URL.
		ctx, cancel := context.WithTimeout(context.Background(), instagramFetchTimeout)
		resolved, err := igClient.ResolveShare(ctx, u.String())
		cancel()
		if err != nil {
			log.Printf("Instagram: could not resolve share link %s: %v", u, err)
			return out
		}
		canonical, err := url.Parse(instagram.PostURL(resolved.Shortcode, resolved.IsReel))
		if err != nil {
			return out
		}
		out.url, out.changed, link = canonical, true, resolved
	}
	if cleaned := cleanInstagramURL(out.url); cleaned.String() != out.url.String() {
		out.url, out.changed = cleaned, true
	}
	if link.Kind != instagram.KindPost {
		return out
	}

	if fetchMedia && instagramLocalFetch {
		media, note, err := fetchInstagramMedia(link.Shortcode)
		if err == nil {
			out.media, out.note, out.changed = media, note, true
			return out
		}
		log.Printf("Instagram: local parsing failed for %s, sending link instead: %v", link.Shortcode, err)
	}

	if instagramFallbackHost != "" && !strings.EqualFold(out.url.Host, instagramFallbackHost) {
		rewritten := *out.url
		rewritten.Host = instagramFallbackHost
		out.url, out.changed = &rewritten, true
	}
	return out
}

// instagramKeptParams are the only query parameters kept on instagram.com links
// (carousel slide, search query). Instagram keeps adding new tracking tokens
// (igsh, igshid, xtok, mdxt, srtk, ...), so everything else is dropped.
var instagramKeptParams = []string{"img_index", "q"}

// cleanInstagramURL returns a copy of u without tracking parameters or fragment.
func cleanInstagramURL(u *url.URL) *url.URL {
	cleaned := *u
	q := u.Query()
	kept := url.Values{}
	for _, name := range instagramKeptParams {
		if v, ok := q[name]; ok {
			kept[name] = v
		}
	}
	cleaned.RawQuery = kept.Encode()
	cleaned.Fragment, cleaned.RawFragment = "", ""
	return &cleaned
}

// fetchInstagramMedia parses a post and downloads all of its photos and videos.
// It fails unless every item downloads, so a carousel is never sent half-empty.
func fetchInstagramMedia(shortcode string) ([]mediaFile, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), instagramFetchTimeout)
	defer cancel()
	post, err := igClient.FetchPost(ctx, shortcode)
	if err != nil {
		return nil, "", err
	}
	files, err := downloadInstagramItems(post.Items)
	if err != nil {
		return nil, "", err
	}
	return files, instagramNote(post), nil
}

func downloadInstagramItems(items []instagram.MediaItem) ([]mediaFile, error) {
	header := http.Header{}
	header.Set("User-Agent", instagram.UserAgent)
	header.Set("Referer", "https://www.instagram.com/")

	sem := make(chan struct{}, maxPhotosPerMessage)
	var wg sync.WaitGroup
	files := make([]mediaFile, len(items))
	errs := make([]error, len(items))

	for i, item := range items {
		wg.Add(1)
		go func(idx int, item instagram.MediaItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			opts := mediaDownload{dir: imageCacheDir, defaultExt: ".jpg", client: httpClient, maxBytes: maxPhotoUploadBytes, header: header}
			if item.Type == instagram.Video {
				opts = mediaDownload{dir: videoCacheDir, defaultExt: ".mp4", client: videoHTTPClient, maxBytes: maxVideoUploadBytes, header: header}
			}
			path, err := downloadMedia(item.URL, opts)
			files[idx] = mediaFile{Path: path, IsVideo: item.Type == instagram.Video, Width: item.Width, Height: item.Height}
			errs[idx] = err
		}(i, item)
	}
	wg.Wait()

	for _, err := range errs {
		if err == nil {
			continue
		}
		var downloaded []mediaFile
		for i := range files {
			if errs[i] == nil {
				downloaded = append(downloaded, files[i])
			}
		}
		removeMediaFiles(downloaded)
		return nil, err
	}
	return files, nil
}

// instagramNote describes an Instagram post for the media caption.
func instagramNote(post *instagram.Post) string {
	// Instagram captions often use U+2063 (invisible separator) for spacing.
	return postNote("Instagram", post.Username, strings.ReplaceAll(post.Caption, "\u2063", ""))
}
