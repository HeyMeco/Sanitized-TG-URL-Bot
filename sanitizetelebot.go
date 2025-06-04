package main

import (
	"bufio"
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

// Constants for various strings and configurations
const (
	telegramTokenEnvVar = "TELEGRAM_BOT_TOKEN"
	tokenFileName       = "token.txt"
	imageCacheDir       = "image_cache"

	tiktokShortHost        = "vm.tiktok.com"
	tiktokHost             = "tiktok.com"
	tiktokHostSuffix       = "tiktok.com" // Used with strings.HasSuffix
	tiktokPhotoPathSegment = "/photo/"
	tiktokLivePathSegment  = "/live"
	tiktokCleanHost        = "vm.dstn.to"

	xComHost   = "x.com"
	fixupXHost = "fixupx.com"

	instagramHostSuffix         = "instagram.com"
	instagramProfileCardSegment = "profilecard" // Path segment: /username/profilecard
	instagramReelPathSegment    = "/reel/"
	instagramPostPathSegment    = "/p/"
	ddInstagramHost             = "d.ddinstagram.com"

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

	log.Println("Bot is starting...")
	b.Start()
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

	sanitizedMsg, wasSanitized, isTikTokPhotoAlbum, downloadedPhotoPaths, originalURLs, err := sanitizeURL(messageText)
	if err != nil {
		log.Printf("Error sanitizing URL for text from user %s ('%s'): %v", username, messageText, err)
		// Notify user about the error, optionally.
		// c.Reply("Sorry, I couldn't process your message due to an internal error.")
		return err // Propagate error to telebot library, it might log it or handle it.
	}

	if !wasSanitized {
		return nil // No URLs were changed or special actions taken.
	}

	sendOpts := &tele.SendOptions{ParseMode: tele.ModeMarkdown}
	if c.Message().IsReply() && c.Message().ReplyTo != nil {
		sendOpts.ReplyTo = c.Message().ReplyTo
	}

	if len(originalURLs) > 0 {
		buttons := createURLButtons(originalURLs)
		sendOpts.ReplyMarkup = &tele.ReplyMarkup{InlineKeyboard: buttons}
	}

	var sendErr error
	if isTikTokPhotoAlbum && len(downloadedPhotoPaths) > 0 {
		// Define the maximum number of photos per message
		const maxPhotosPerMessage = 10

		// Prepare the base caption text
		var baseCaption string
		if c.Message().FromGroup() && strings.Contains(sanitizedMsg, msgMarkerAnon) {
			baseCaption = strings.Replace(sanitizedMsg, msgMarkerAnon, "", 1)
		} else {
			baseCaption = "@" + username + " said: " + sanitizedMsg
		}

		// Calculate total number of parts
		totalParts := (len(downloadedPhotoPaths) + maxPhotosPerMessage - 1) / maxPhotosPerMessage

		// Split photos into groups of 10
		for i := 0; i < len(downloadedPhotoPaths); i += maxPhotosPerMessage {
			end := i + maxPhotosPerMessage
			if end > len(downloadedPhotoPaths) {
				end = len(downloadedPhotoPaths)
			}

			// Create album for this batch
			album := make(tele.Album, 0, maxPhotosPerMessage)
			for j, photoPath := range downloadedPhotoPaths[i:end] {
				photo := &tele.Photo{File: tele.FromDisk(photoPath)}
				if j == 0 { // Add caption to first photo of each album
					partNum := (i / maxPhotosPerMessage) + 1
					captionText := baseCaption
					if partNum > 1 { // Add part number for all parts except the first
						captionText = fmt.Sprintf("%s (Part %d/%d)", baseCaption, partNum, totalParts)
					} else if totalParts > 1 { // For first part, only add number if there are multiple parts
						captionText = fmt.Sprintf("%s (Part 1/%d)", baseCaption, totalParts)
					}
					photo.Caption = escapeMarkdown(captionText)
				}
				album = append(album, photo)
			}

			// Send this batch
			_, batchErr := b.SendAlbum(c.Chat(), album, sendOpts)
			if batchErr != nil {
				sendErr = fmt.Errorf("failed to send photo batch %d-%d: %w", i+1, end, batchErr)
				break // Stop sending more batches if one fails
			}
		}

		// Clean up downloaded images after attempting to send all batches
		for _, photoPath := range downloadedPhotoPaths {
			if rmErr := os.Remove(photoPath); rmErr != nil {
				log.Printf("Failed to remove cached image %s: %v", photoPath, rmErr)
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
	sanitizedMsg, wasSanitized, _, _, _, err := sanitizeURL(queryText)
	if err != nil {
		log.Printf("Error sanitizing URL for inline query '%s': %v", queryText, err)
		return err
	}

	if wasSanitized {
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

func sanitizeURL(text string) (sanitizedText string, wasSanitized bool, isTikTokPhotoAlbum bool, downloadedPhotoPaths []string, originalURLs []string, err error) {
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

			originalURLs = append(originalURLs, word)
			currentWordSanitized := false
			processedWord := word

			parsedURL, parseErr := url.Parse(word)
			if parseErr != nil {
				log.Printf("Warning: Failed to parse URL '%s': %v. Using original.", word, parseErr)
				sb.WriteString(word)
				continue
			}

			// --- TikTok URL Expansion ---
			if parsedURL.Host == tiktokShortHost || (parsedURL.Host == tiktokHost && !strings.Contains(parsedURL.Path, "/t/")) {
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
				isTikTokPhotoAlbum = true                                         // Mark that this type of URL was encountered
				tempPhotoPaths, fetchErr := fetchTikTokPhotos(parsedURL.String()) // Uses global httpClient
				if fetchErr != nil {
					log.Printf("Warning: Failed to fetch TikTok photos for '%s': %v. URL params will be cleaned, but no album.", parsedURL.String(), fetchErr)
					isTikTokPhotoAlbum = false // Reset if fetching fails, it's not an album then
				} else {
					downloadedPhotoPaths = append(downloadedPhotoPaths, tempPhotoPaths...)
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
						if parsedURL.Host != tiktokCleanHost {
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
					if strings.Contains(parsedURL.Path, instagramReelPathSegment) || strings.Contains(parsedURL.Path, instagramPostPathSegment) {
						if parsedURL.Host != ddInstagramHost {
							parsedURL.Host = ddInstagramHost
							processedWord = parsedURL.String()
							currentWordSanitized = true
						}
					}
				}
			}
			sb.WriteString(processedWord)
			if currentWordSanitized {
				wasSanitized = true
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return "", false, false, nil, nil, fmt.Errorf("error scanning input text: %w", scanErr)
	}

	// If it was marked as a TikTok photo album opportunity AND photos were actually downloaded,
	// then it counts as "sanitized" (because an action is taken).
	if isTikTokPhotoAlbum && len(downloadedPhotoPaths) > 0 {
		wasSanitized = true
	} else {
		// If photo download failed, ensure isTikTokPhotoAlbum is false so it's not treated as an album.
		isTikTokPhotoAlbum = false
	}

	return sb.String(), wasSanitized, isTikTokPhotoAlbum, downloadedPhotoPaths, originalURLs, nil
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
	if err := os.MkdirAll(imageCacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create image cache directory %s: %w", imageCacheDir, err)
	}

	hasher := sha256.New()
	hasher.Write([]byte(imageURL))
	hashStr := hex.EncodeToString(hasher.Sum(nil))[:16] // Short hash for filename

	parsedURLForExt, err := url.Parse(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse image URL %s for extension: %w", imageURL, err)
	}
	ext := filepath.Ext(parsedURLForExt.Path)
	if ext == "" {
		ext = ".jpg" // Default extension
	}

	filename := filepath.Join(imageCacheDir, fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), hashStr, ext))

	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create GET request for image %s: %w", imageURL, err)
	}
	// req.Header.Set("User-Agent", "...") // Optional: if server requires specific UA

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to start download for %s: %w", imageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image %s: status %s", imageURL, resp.Status)
	}

	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create image file %s: %w", filename, err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		_ = os.Remove(filename) // Attempt to clean up partially written file
		return "", fmt.Errorf("failed to write image data to %s: %w", filename, err)
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
