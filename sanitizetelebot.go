package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tele "gopkg.in/telebot.v4"
)

// TikwmResponse represents the JSON response from tikwm.com API
type TikwmResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data Data   `json:"data"`
}

// Data holds the actual data from TikwmResponse
type Data struct {
	Images []string `json:"images"`
}

// Global HTTP client for making external requests with a timeout.
var httpClient = &http.Client{
	Timeout: 20 * time.Second, // Adjusted for potentially slow operations like multiple image downloads
}

// videoHTTPClient is used for video downloads, which can take longer than a page fetch.
var videoHTTPClient = &http.Client{
	Timeout: 90 * time.Second,
}

// mediaFile is a downloaded photo or video waiting to be sent to Telegram.
type mediaFile struct {
	Path    string
	IsVideo bool
	Width   int
	Height  int
}

// sanitizeResult is everything sanitizeURL found out about a message.
type sanitizeResult struct {
	Text         string      // Message text with cleaned URLs.
	WasSanitized bool        // True if the text changed or media was downloaded.
	Media        []mediaFile // Downloaded media (TikTok photos, Instagram posts) to send as an album.
	MediaNotes   []string    // Extra caption lines describing the media (e.g. Instagram author and caption).
	OriginalURLs []string
}

// Constants for various strings and configurations
const (
	telegramTokenEnvVar = "TELEGRAM_BOT_TOKEN"
	tokenFileName       = "token.txt"
	imageCacheDir       = "image_cache"
	videoCacheDir       = "video_cache"

	// Telegram Bot API upload limits (multipart uploads).
	maxPhotoUploadBytes = 10 << 20
	maxVideoUploadBytes = 50 << 20
	// Telegram limits media captions to 1024 characters and albums to 10 items.
	maxCaptionRunes     = 1024
	maxPhotosPerMessage = 10

	tiktokShortHost        = "vm.tiktok.com"
	tiktokProHost          = "pro.tiktok.com"
	tiktokHost             = "tiktok.com"
	tiktokHostSuffix       = "tiktok.com" // Used with strings.HasSuffix
	tiktokPhotoPathSegment = "/photo/"
	tiktokLivePathSegment  = "/live"
	tiktokCleanHost        = "vm.dstn.to"

	xComHost   = "x.com"
	fixupXHost = "fixupx.com"

	instagramHostSuffix         = "instagram.com"
	instagramProfileCardSegment = "profilecard" // Path segment: /username/profilecard

	msgMarkerAnon        = "anon"
	msgMarkerNoCut       = "nocut"
	inlineQueryDefaultID = "clearurl_result_1" // More specific ID
)

// markdownEscaper is a reusable strings.Replacer for escaping Markdown characters.
var markdownEscaper = strings.NewReplacer(
	"[", "\\[", "]", "\\]",
	"_", "\\_", "*", "\\*",
	"`", "\\`",
)

func main() {
	tokenStr := loadTelegramToken()
	if tokenStr == "" {
		log.Fatal("Error: Telegram bot token is empty or could not be loaded. Please provide a valid token via TELEGRAM_BOT_TOKEN env var or token.txt file.")
	}
	// Errors from the Telegram API include request URLs, which contain the token.
	log.SetOutput(&redactingWriter{w: os.Stderr, secret: tokenStr})

	pref := tele.Settings{
		Token:  tokenStr,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	b.Handle(tele.OnText, func(c tele.Context) error {
		return handleTextMessage(c, b)
	})

	b.Handle(tele.OnQuery, func(c tele.Context) error {
		return handleInlineQuery(c, b)
	})

	// NewBot already called getMe, so the token works and Telegram is reachable.
	log.Printf("Connected to Telegram as @%s (id %d). Listening for messages...", b.Me.Username, b.Me.ID)
	log.Printf("Instagram: local fetch %v, fallback host %q", instagramLocalFetch, instagramFallbackHost)
	b.Start()
}

// redactingWriter replaces every occurrence of secret before writing to w.
// The log package writes each entry with a single Write call, so a secret is
// never split across writes.
type redactingWriter struct {
	w      io.Writer
	secret string
}

func (r *redactingWriter) Write(p []byte) (int, error) {
	if r.secret == "" || !bytes.Contains(p, []byte(r.secret)) {
		return r.w.Write(p)
	}
	if _, err := r.w.Write(bytes.ReplaceAll(p, []byte(r.secret), []byte("<redacted>"))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func loadTelegramToken() string {
	tokenStr := os.Getenv(telegramTokenEnvVar)
	if tokenStr != "" {
		log.Println("Loaded Telegram token from environment variable.")
		return tokenStr
	}

	file, err := os.Open(tokenFileName)
	if err != nil {
		log.Printf("Warning: %s not found (%v). Checking env var was the only option if this fails.", tokenFileName, err)
		return ""
	}
	defer file.Close()

	tokenBytes, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Warning: Failed to read %s: %v", tokenFileName, err)
		return ""
	}
	log.Printf("Loaded Telegram token from %s.", tokenFileName)
	return strings.TrimSpace(string(tokenBytes))
}

func handleTextMessage(c tele.Context, b *tele.Bot) error {
	sender := c.Sender()
	if sender == nil {
		log.Println("Warning: Received message without sender information.")
		return nil // Or handle as an error by returning an error
	}
	username := getUsername(sender)
	messageText := c.Text()

	if strings.Contains(messageText, msgMarkerNoCut) {
		return nil // "nocut" keyword present, do nothing.
	}

	res, err := sanitizeURL(messageText, true)
	if err != nil {
		log.Printf("Error sanitizing URL for text from user %s ('%s'): %v", username, messageText, err)
		// Notify user about the error, optionally.
		// c.Reply("Sorry, I couldn't process your message due to an internal error.")
		return err // Propagate error to telebot library, it might log it or handle it.
	}
	// Clean up downloaded media however sending goes.
	defer removeMediaFiles(res.Media)

	if !res.WasSanitized {
		return nil // No URLs were changed or special actions taken.
	}
	sanitizedMsg := res.Text

	sendOpts := &tele.SendOptions{ParseMode: tele.ModeMarkdown}
	if c.Message().IsReply() && c.Message().ReplyTo != nil {
		sendOpts.ReplyTo = c.Message().ReplyTo
	}

	if len(res.OriginalURLs) > 0 {
		buttons := createURLButtons(res.OriginalURLs)
		sendOpts.ReplyMarkup = &tele.ReplyMarkup{InlineKeyboard: buttons}
	}

	var sendErr error
	if len(res.Media) > 0 {
		// Media captions use HTML so the post description can be shown as a blockquote.
		mediaOpts := *sendOpts
		mediaOpts.ParseMode = tele.ModeHTML
		// Prepare the base caption text
		var baseCaption string
		if c.Message().FromGroup() && strings.Contains(sanitizedMsg, msgMarkerAnon) {
			baseCaption = strings.Replace(sanitizedMsg, msgMarkerAnon, "", 1)
		} else {
			baseCaption = "@" + username + " said: " + sanitizedMsg
		}

		// Calculate total number of parts
		totalParts := (len(res.Media) + maxPhotosPerMessage - 1) / maxPhotosPerMessage

		// Split media into groups of 10
		for i := 0; i < len(res.Media); i += maxPhotosPerMessage {
			end := i + maxPhotosPerMessage
			if end > len(res.Media) {
				end = len(res.Media)
			}

			partNum := (i / maxPhotosPerMessage) + 1
			captionText := baseCaption
			if totalParts > 1 {
				captionText = fmt.Sprintf("%s (Part %d/%d)", baseCaption, partNum, totalParts)
			}
			var notes []string
			if partNum == 1 { // Only the first part carries the post descriptions
				notes = res.MediaNotes
			}
			caption := buildMediaCaption(captionText, notes)

			// Create album for this batch; the caption goes on the first item
			album := make(tele.Album, 0, end-i)
			for j, m := range res.Media[i:end] {
				itemCaption := ""
				if j == 0 {
					itemCaption = caption
				}
				album = append(album, m.inputMedia(itemCaption))
			}

			// Telegram media groups need at least two items, so send single items directly.
			var batchErr error
			if len(album) == 1 {
				_, batchErr = b.Send(c.Chat(), album[0], &mediaOpts)
			} else {
				_, batchErr = b.SendAlbum(c.Chat(), album, &mediaOpts)
			}
			if batchErr != nil {
				sendErr = fmt.Errorf("failed to send media batch %d-%d: %w", i+1, end, batchErr)
				break // Stop sending more batches if one fails
			}
		}
	} else {
		var messageToSend string
		if c.Message().FromGroup() && strings.Contains(sanitizedMsg, msgMarkerAnon) { // Check original sanitizedMsg for "anon"
			messageToSend = strings.Replace(sanitizedMsg, msgMarkerAnon, "", 1)
		} else {
			messageToSend = "@" + username + " said: " + sanitizedMsg
		}
		_, sendErr = b.Send(c.Chat(), escapeMarkdown(messageToSend), sendOpts)
	}

	if sendErr != nil {
		log.Printf("Failed to send sanitized message to chat %d: %v", c.Chat().ID, sendErr)
		return sendErr
	}

	// Successfully sent the new message, now delete the original.
	if err := b.Delete(c.Message()); err != nil {
		log.Printf("Failed to delete original message (ID: %d, ChatID: %d): %v", c.Message().ID, c.Chat().ID, err)
		// Not returning this error as critical because the main operation (sending sanitized message) succeeded.
	}
	return nil
}

func handleInlineQuery(c tele.Context, b *tele.Bot) error {
	queryText := c.Query().Text
	// Inline results are text only, so don't download any media here.
	res, err := sanitizeURL(queryText, false)
	if err != nil {
		log.Printf("Error sanitizing URL for inline query '%s': %v", queryText, err)
		return err
	}
	sanitizedMsg := res.Text

	if res.WasSanitized {
		result := &tele.ArticleResult{
			Title:       "Sanitized URL",                // Could be more dynamic, e.g., show the cleaned URL snippet
			Text:        sanitizedMsg,                   // This is MessageText, which is sent when user selects the result
			Description: "Tap to send the cleaned URL.", // Shown in the results list
		}
		result.SetResultID(inlineQueryDefaultID) // ID should be unique if you plan to have multiple results

		results := []tele.Result{result}
		resp := &tele.QueryResponse{
			Results:   results,
			CacheTime: 60, // Optional: How long (in seconds) the Telegram client should cache this result.
		}

		if err := b.Answer(c.Query(), resp); err != nil {
			log.Printf("Failed to answer inline query for query ID %s: %v", c.Query().ID, err)
			return err
		}
	}
	// If not sanitized, bot sends no results, which is fine.
	return nil
}

func getUsername(sender *tele.User) string {
	if sender.Username != "" {
		return sender.Username
	}
	return sender.FirstName // Fallback to FirstName if username is not set
}

// sanitizeURL cleans every URL in text. With fetchMedia set it also downloads
// media for TikTok photo posts and Instagram posts; the caller must remove the
// returned files (see removeMediaFiles).
func sanitizeURL(text string, fetchMedia bool) (sanitizeResult, error) {
	var res sanitizeResult
	var sb strings.Builder
	sb.Grow(len(text) + 64) // Pre-allocate: original length + buffer for prefixes/changes

	scanner := bufio.NewScanner(strings.NewReader(text))
	isFirstParagraph := true

	for scanner.Scan() {
		paragraph := scanner.Text()
		if !isFirstParagraph {
			sb.WriteByte('\n')
		}
		isFirstParagraph = false

		if strings.TrimSpace(paragraph) == "" { // Preserve empty lines by only writing \n if it's not the first
			if !isFirstParagraph { // if it was truly an empty line after a non-empty one
				// sb.WriteByte('\n') // Already handled by the start of loop logic
			}
			continue
		}

		words := strings.Fields(paragraph)
		isFirstWordInParagraph := true

		for _, word := range words {
			if !isFirstWordInParagraph {
				sb.WriteByte(' ')
			}
			isFirstWordInParagraph = false

			if !containsURL(word) {
				sb.WriteString(word)
				continue
			}

			res.OriginalURLs = append(res.OriginalURLs, word)
			currentWordSanitized := false
			processedWord := word

			parsedURL, parseErr := url.Parse(word)
			if parseErr != nil {
				log.Printf("Warning: Failed to parse URL '%s': %v. Using original.", word, parseErr)
				sb.WriteString(word)
				continue
			}

			// --- TikTok URL Expansion ---
			if parsedURL.Host == tiktokShortHost || parsedURL.Host == tiktokProHost || parsedURL.Host == tiktokHost {
				expandedURLStr, expandErr := ExpandUrl(parsedURL.String()) // Uses global httpClient
				if expandErr != nil {
					log.Printf("Warning: Failed to expand TikTok URL '%s': %v. Proceeding with unexpanded.", parsedURL.String(), expandErr)
				} else {
					expandedParsedURL, parseExpandedErr := url.Parse(expandedURLStr)
					if parseExpandedErr != nil {
						log.Printf("Warning: Failed to parse expanded TikTok URL '%s': %v. Proceeding with unexpanded original.", expandedURLStr, parseExpandedErr)
					} else {
						if parsedURL.String() != expandedParsedURL.String() { // If expansion changed the URL
							currentWordSanitized = true
						}
						parsedURL = expandedParsedURL
						processedWord = parsedURL.String()
					}
				}
			}

			// --- TikTok Photo Album Processing (after potential expansion) ---
			if strings.HasSuffix(parsedURL.Host, tiktokHostSuffix) && strings.Contains(parsedURL.Path, tiktokPhotoPathSegment) {
				if fetchMedia {
					tempPhotoPaths, fetchErr := fetchTikTokPhotos(parsedURL.String()) // Uses global httpClient
					if fetchErr != nil {
						log.Printf("Warning: Failed to fetch TikTok photos for '%s': %v. URL params will be cleaned, but no album.", parsedURL.String(), fetchErr)
					} else {
						for _, photoPath := range tempPhotoPaths {
							res.Media = append(res.Media, mediaFile{Path: photoPath})
						}
					}
				}

				if parsedURL.RawQuery != "" { // Always remove query params for TikTok photo URLs
					parsedURL.RawQuery = ""
					currentWordSanitized = true
				}
				processedWord = parsedURL.String()
			} else {
				// --- General Parameter Cleaning and Host Replacements (for non-TikTok photo URLs) ---
				q := parsedURL.Query()
				paramsModified := false

				for paramName := range q { // Universal rules
					for _, rulePrefix := range URLRules {
						if strings.HasPrefix(paramName, rulePrefix) {
							q.Del(paramName)
							paramsModified = true
						}
					}
				}
				for domainKey, rulePrefixes := range DomainRules { // Domain-specific rules
					if strings.Contains(parsedURL.Host, domainKey) { // `domainKey` could be "amazon" matching "amazon.co.uk"
						for paramName := range q {
							for _, rulePrefix := range rulePrefixes {
								if strings.HasPrefix(paramName, rulePrefix) {
									q.Del(paramName)
									paramsModified = true
								}
							}
						}
					}
				}
				if paramsModified {
					parsedURL.RawQuery = q.Encode()
					processedWord = parsedURL.String()
					currentWordSanitized = true
				}

				// --- Special Domain Replacements ---
				if strings.HasSuffix(parsedURL.Host, tiktokHostSuffix) { // TikTok non-photo/live
					if !strings.Contains(parsedURL.Path, tiktokPhotoPathSegment) && !strings.Contains(parsedURL.Path, tiktokLivePathSegment) {
						if parsedURL.Host != tiktokCleanHost && strings.Contains(parsedURL.Path, "/video/") {
							parsedURL.Host = tiktokCleanHost
							processedWord = parsedURL.String()
							currentWordSanitized = true
						}
					}
					if strings.Contains(parsedURL.Path, tiktokLivePathSegment) && parsedURL.RawQuery != "" { // TikTok Live
						parsedURL.RawQuery = ""
						processedWord = parsedURL.String()
						currentWordSanitized = true
					}
				}
				if parsedURL.Host == xComHost && parsedURL.Host != fixupXHost { // X.com
					parsedURL.Host = fixupXHost
					processedWord = parsedURL.String()
					currentWordSanitized = true
				}
				if strings.HasSuffix(parsedURL.Host, instagramHostSuffix) { // Instagram
					pathSegments := strings.Split(parsedURL.Path, "/")
					if len(pathSegments) > 2 && pathSegments[2] == instagramProfileCardSegment { // /username/profilecard/...
						parsedURL.Path = "/" + pathSegments[1] // Becomes /username
						processedWord = parsedURL.String()
						currentWordSanitized = true
					}
				}
				if ig := processInstagramURL(parsedURL, fetchMedia); ig.changed {
					parsedURL = ig.url
					processedWord = parsedURL.String()
					currentWordSanitized = true
					res.Media = append(res.Media, ig.media...)
					if ig.note != "" {
						res.MediaNotes = append(res.MediaNotes, ig.note)
					}
				}
			}
			sb.WriteString(processedWord)
			if currentWordSanitized {
				res.WasSanitized = true
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		removeMediaFiles(res.Media)
		return sanitizeResult{}, fmt.Errorf("error scanning input text: %w", scanErr)
	}

	// Downloaded media counts as "sanitized" because the bot will repost it.
	if len(res.Media) > 0 {
		res.WasSanitized = true
	}
	res.Text = sb.String()
	return res, nil
}

func containsURL(text string) bool {
	return strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")
}

func ExpandUrl(shortURL string) (string, error) {
	req, err := http.NewRequest("HEAD", shortURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HEAD request for %s: %w", shortURL, err)
	}
	// req.Header.Set("User-Agent", "Mozilla/5.0...") // Optional: Set User-Agent if needed

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HEAD request failed for %s: %w", shortURL, err)
	}
	defer resp.Body.Close()

	// We are interested in the final URL from resp.Request.URL after redirects.
	// Default client follows redirects for HEAD.
	if resp.StatusCode >= http.StatusBadRequest { // 400 and above are generally errors
		return "", fmt.Errorf("received non-successful status code %d for %s", resp.StatusCode, shortURL)
	}
	return resp.Request.URL.String(), nil
}

func escapeMarkdown(text string) string {
	return markdownEscaper.Replace(text)
}

func downloadImage(imageURL string) (string, error) {
	return downloadMedia(imageURL, mediaDownload{dir: imageCacheDir, defaultExt: ".jpg", client: httpClient})
}

// mediaDownload configures downloadMedia.
type mediaDownload struct {
	dir        string
	defaultExt string
	client     *http.Client
	maxBytes   int64       // 0 means unlimited.
	header     http.Header // Extra request headers, may be nil.
}

// downloadMedia saves mediaURL into opts.dir and returns the file path.
func downloadMedia(mediaURL string, opts mediaDownload) (string, error) {
	if err := os.MkdirAll(opts.dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory %s: %w", opts.dir, err)
	}

	hasher := sha256.New()
	hasher.Write([]byte(mediaURL))
	hashStr := hex.EncodeToString(hasher.Sum(nil))[:16] // Short hash for filename

	parsedURLForExt, err := url.Parse(mediaURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse media URL %s for extension: %w", mediaURL, err)
	}
	ext := filepath.Ext(parsedURLForExt.Path)
	if ext == "" {
		ext = opts.defaultExt
	}

	filename := filepath.Join(opts.dir, fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), hashStr, ext))

	req, err := http.NewRequest("GET", mediaURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create GET request for %s: %w", mediaURL, err)
	}
	for k, v := range opts.header {
		req.Header[k] = v
	}

	resp, err := opts.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to start download for %s: %w", mediaURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: status %s", mediaURL, resp.Status)
	}
	if opts.maxBytes > 0 && resp.ContentLength > opts.maxBytes {
		return "", fmt.Errorf("%s is too large (%d bytes, limit %d)", mediaURL, resp.ContentLength, opts.maxBytes)
	}

	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	defer file.Close()

	body := io.Reader(resp.Body)
	if opts.maxBytes > 0 {
		body = io.LimitReader(resp.Body, opts.maxBytes+1)
	}
	written, err := io.Copy(file, body)
	if err != nil {
		_ = os.Remove(filename) // Attempt to clean up partially written file
		return "", fmt.Errorf("failed to write data to %s: %w", filename, err)
	}
	if opts.maxBytes > 0 && written > opts.maxBytes {
		_ = os.Remove(filename)
		return "", fmt.Errorf("%s is larger than the %d byte limit", mediaURL, opts.maxBytes)
	}
	return filename, nil
}

func fetchTikTokPhotos(photoPostURL string) ([]string, error) {
	apiURL := fmt.Sprintf("https://tikwm.com/api?url=%s&hd=1&cursor=0", url.QueryEscape(photoPostURL))
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for tikwm API: %w", err)
	}
	// req.Header.Set("User-Agent", "...") // Optional: if tikwm API requires it

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tikwm API request failed for %s: %w", photoPostURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tikwm API for %s returned status %s", photoPostURL, resp.Status)
	}

	var tikwmResp TikwmResponse
	if err := json.NewDecoder(resp.Body).Decode(&tikwmResp); err != nil {
		return nil, fmt.Errorf("failed to decode tikwm API response for %s: %w", photoPostURL, err)
	}

	if tikwmResp.Code != 0 {
		return nil, fmt.Errorf("tikwm API error for %s (code %d): %s", photoPostURL, tikwmResp.Code, tikwmResp.Msg)
	}
	if len(tikwmResp.Data.Images) == 0 {
		// This can happen if a video link is passed to a photo endpoint, or if the post has no images.
		return nil, fmt.Errorf("tikwm API returned no images for %s (Code: %d, Msg: %s)", photoPostURL, tikwmResp.Code, tikwmResp.Msg)
	}

	maxConcurrentDownloads := 10 // Limit concurrency to avoid overwhelming servers/network
	sem := make(chan struct{}, maxConcurrentDownloads)
	var wg sync.WaitGroup

	// Using slice of struct to hold path and error together for easier processing
	type downloadResult struct {
		path string
		err  error
	}
	results := make([]downloadResult, len(tikwmResp.Data.Images))

	for i, imgURL := range tikwmResp.Data.Images {
		wg.Add(1)
		go func(idx int, urlToDownload string) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			localPath, downloadErr := downloadImage(urlToDownload)
			results[idx] = downloadResult{path: localPath, err: downloadErr}
			if downloadErr != nil {
				log.Printf("Failed to download TikTok image %s (source: %s): %v", urlToDownload, photoPostURL, downloadErr)
			}
		}(i, imgURL)
	}
	wg.Wait()

	successfulPaths := make([]string, 0, len(tikwmResp.Data.Images))
	var firstErr error
	for _, res := range results {
		if res.err == nil && res.path != "" {
			successfulPaths = append(successfulPaths, res.path)
		} else if res.err != nil && firstErr == nil {
			firstErr = res.err // Capture the first download error encountered
		}
	}

	if len(successfulPaths) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("all image downloads failed for %s; first error: %w", photoPostURL, firstErr)
		}
		return nil, fmt.Errorf("no images were successfully downloaded for %s, though API indicated images were present", photoPostURL)
	}
	return successfulPaths, nil
}

func createURLButtons(urls []string) [][]tele.InlineButton {
	if len(urls) == 0 {
		return nil
	}
	rows := make([][]tele.InlineButton, 0, len(urls)) // Pre-allocate
	for i, u := range urls {
		// Ensure URL is valid for Telegram button (absolute, well-formed)
		// Telegram might reject malformed URLs. url.Parse check could be added here if needed.
		parsedU, err := url.ParseRequestURI(u)
		if err != nil || !parsedU.IsAbs() {
			log.Printf("Warning: Skipping invalid URL for button '%s': %v", u, err)
			continue
		}
		btn := tele.InlineButton{
			Text: fmt.Sprintf("Original Link #%d", i+1),
			URL:  u,
		}
		rows = append(rows, []tele.InlineButton{btn})
	}
	if len(rows) == 0 {
		return nil
	} // If all URLs were invalid
	return rows
}
