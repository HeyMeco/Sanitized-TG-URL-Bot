// v2 uses the rules from https://github.com/Vendicated/Vencord/tree/main/src/plugins/clearURLs so credits to them
package main

import (
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
	"time"

	telebot "gopkg.in/tucnak/telebot.v2"
)

// TikwmResponse represents the JSON response from tikwm.com API
type TikwmResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data Data   `json:"data"`
}

type Data struct {
	Images []string `json:"images"`
}

func main() {
	var tokenStr string

	// Check for environment variable first
	tokenStr = os.Getenv("TELEGRAM_BOT_TOKEN")

	// If environment variable is empty, try to read from file
	if tokenStr == "" {
		file, err := os.Open("token.txt")
		if err != nil {
			log.Fatal("Error: token.txt is missing or if run as a docker image, the TELEGRAM_BOT_TOKEN environment variable is missing")
			return
		}
		defer file.Close()

		token, err := io.ReadAll(file)
		if err != nil {
			log.Fatal(err)
			return
		}
		// Trim the newline character from the token string
		tokenStr = strings.TrimSpace(string(token))
	}

	if tokenStr == "" {
		log.Fatal("Error: Telegram bot token is empty. Please provide a valid token.")
		return
	}

	b, err := telebot.NewBot(telebot.Settings{
		Token:  tokenStr,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})

	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle(telebot.OnText, func(m *telebot.Message) {
		username := getUsername(m.Sender)

		if strings.Contains(m.Text, "nocut") {
			return
		}

		sanitizedMsg, sanitized, isPhotoURL, photoURLs, originalURLs, err := sanitizeURL(m.Text)
		if err != nil {
			log.Println(err)
			return
		}

		if sanitized {
			// Create send options with reply if original was a reply
			sendOpts := &telebot.SendOptions{
				ParseMode: telebot.ModeMarkdown,
			}
			if m.IsReply() {
				sendOpts.ReplyTo = m.ReplyTo
			}

			if isPhotoURL && len(photoURLs) > 0 {
				// Create URL buttons for photo albums
				if len(originalURLs) > 0 {
					buttons := createURLButtons(originalURLs)
					sendOpts.ReplyMarkup = &telebot.ReplyMarkup{InlineKeyboard: buttons}
				}

				// Create album of photos with caption on first photo
				album := make(telebot.Album, 0)
				for i, photoPath := range photoURLs {
					var photo *telebot.Photo
					if i == 0 {
						// Get the message text in the default format
						messageText := ""
						escapedMsg := escapeMarkdown(sanitizedMsg)
						if m.FromGroup() && strings.Contains(sanitizedMsg, "anon") {
							messageText = strings.Replace(escapedMsg, "anon", "", 1)
						} else {
							messageText = "@" + username + " said: " + escapedMsg
						}
						photo = &telebot.Photo{
							File:    telebot.FromDisk(photoPath),
							Caption: messageText,
						}
					} else {
						photo = &telebot.Photo{File: telebot.FromDisk(photoPath)}
					}
					album = append(album, photo)
				}

				// Send the album with reply and buttons
				_, err := b.SendAlbum(m.Chat, album, sendOpts)
				if err != nil {
					log.Printf("Failed to send album: %v", err)
				}

				// Clean up the cached images
				for _, photoPath := range photoURLs {
					os.Remove(photoPath)
				}
			} else {
				// Create URL buttons for regular messages
				if len(originalURLs) > 0 {
					buttons := createURLButtons(originalURLs)
					sendOpts.ReplyMarkup = &telebot.ReplyMarkup{InlineKeyboard: buttons}
				}

				var err error
				// Escape any Markdown special characters in the sanitized URL
				escapedMsg := escapeMarkdown(sanitizedMsg)

				if m.FromGroup() && strings.Contains(sanitizedMsg, "anon") {
					_, err = b.Send(m.Chat, strings.Replace(escapedMsg, "anon", "", 1), sendOpts)
				} else {
					_, err = b.Send(m.Chat, "@"+username+" said: "+escapedMsg, sendOpts)
				}
				if err != nil {
					log.Printf("Failed to send message: %v", err)
					return
				}
			}

			// Only try to delete the original message if we successfully sent the new one
			if err := b.Delete(m); err != nil {
				log.Printf("Failed to delete original message: %v", err)
			}
		}
	})

	b.Handle(telebot.OnQuery, func(q *telebot.Query) {
		sanitizedMsg, sanitized, _, _, _, err := sanitizeURL(q.Text)
		if err != nil {
			log.Println(err)
			return
		}

		if sanitized {
			result := &telebot.ArticleResult{
				Title: "Sanitized URL",
				Text:  sanitizedMsg,
			}
			result.SetResultID("1")
			results := []telebot.Result{result}
			err = b.Answer(q, &telebot.QueryResponse{
				Results: results,
			})
			if err != nil {
				log.Println(err)
			}
		}
	})

	log.Println("starting bot")
	b.Start()
}

func getUsername(sender *telebot.User) string {
	if sender.Username == "" {
		return sender.FirstName
	}
	return sender.Username
}

func sanitizeURL(text string) (string, bool, bool, []string, []string, error) {
	// Split text into paragraphs first
	paragraphs := strings.Split(text, "\n")
	var sanitizedParagraphs []string
	var sanitized bool
	var isPhotoURL bool
	var photoURLs []string
	var originalURLs []string

	for _, paragraph := range paragraphs {
		if paragraph == "" {
			sanitizedParagraphs = append(sanitizedParagraphs, "")
			continue
		}

		words := strings.Fields(paragraph)
		var sanitizedWords []string

		for _, word := range words {
			if containsURL(word) {
				originalURLs = append(originalURLs, word) // Store the original URL
				parsedURL, err := url.Parse(word)
				if err != nil {
					sanitizedWords = append(sanitizedWords, word)
					continue
				}

				if parsedURL.Host == "vm.tiktok.com" || parsedURL.Host == "tiktok.com" {
					word, err = ExpandUrl(word)
					if err != nil {
						return "", false, false, nil, nil, err
					}
					parsedURL, err = url.Parse(word)
					if err != nil {
						return "", false, false, nil, nil, err
					}
				}

				// Check if it's a TikTok photo URL
				if strings.HasSuffix(parsedURL.Host, "tiktok.com") && strings.Contains(parsedURL.Path, "/photo/") {
					isPhotoURL = true
					photos, err := fetchTikTokPhotos(parsedURL.String())
					if err != nil {
						log.Printf("Failed to fetch TikTok photos: %v", err)
					} else {
						photoURLs = photos
					}
					// Remove all query parameters from TikTok photo URLs
					parsedURL.RawQuery = ""
					sanitizedWords = append(sanitizedWords, parsedURL.String())
					sanitized = true
					continue
				}

				// Use universal rules from rules.go

				// Clean universal parameters
				q := parsedURL.Query()
				for param := range q {
					for _, rule := range URLRules {
						if strings.HasPrefix(param, rule) {
							q.Del(param)
							sanitized = true
						}
					}
				}

				// Clean host-specific parameters
				for host, rules := range DomainRules {
					if strings.Contains(parsedURL.Host, host) {
						for param := range q {
							for _, rule := range rules {
								if strings.HasPrefix(param, rule) {
									q.Del(param)
									sanitized = true
								}
							}
						}
					}
				}

				// Update URL with cleaned parameters
				parsedURL.RawQuery = q.Encode()

				// Handle special domain replacements
				if strings.HasSuffix(parsedURL.Host, "tiktok.com") {
					// Check if the expanded URL contains "/photo/" or "/live"
					if !strings.Contains(parsedURL.Path, "/photo/") && !strings.Contains(parsedURL.Path, "/live") {
						parsedURL.Host = "vm.dstn.to"
						sanitized = true
					}
					// Remove query parameters if the path contains "/live"
					if strings.Contains(parsedURL.Path, "/live") {
						parsedURL.RawQuery = ""
						sanitized = true
					}
				}
				if parsedURL.Host == "x.com" {
					parsedURL.Host = "fixupx.com"
					sanitized = true
				}
				if strings.HasSuffix(parsedURL.Host, "instagram.com") {
					// Logic to remove "profilecard" path as those result in an error page without share id
					pathSegments := strings.Split(parsedURL.Path, "/")
					if len(pathSegments) > 2 && pathSegments[2] == "profilecard" {
						// Reconstruct the path without the "profilecard" segment
						parsedURL.Path = "/" + pathSegments[1]
						sanitized = true
					}

					// Only rewrite to ddinstagram if path includes "/reel/" or "/p/"
					if strings.Contains(parsedURL.Path, "/reel/") || strings.Contains(parsedURL.Path, "/p/") {
						parsedURL.Host = "d.ddinstagram.com"
						sanitized = true
					}

				}

				sanitizedWords = append(sanitizedWords, parsedURL.String())
			} else {
				sanitizedWords = append(sanitizedWords, word)
			}
		}

		sanitizedParagraphs = append(sanitizedParagraphs, strings.Join(sanitizedWords, " "))
	}

	return strings.Join(sanitizedParagraphs, "\n"), sanitized, isPhotoURL, photoURLs, originalURLs, nil
}

func containsURL(text string) bool {
	return strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")
}

func ExpandUrl(shortURL string) (string, error) {
	resp, err := http.Head(shortURL)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received non-200 status code")
	}
	return resp.Request.URL.String(), nil
}

func escapeMarkdown(text string) string {
	text = strings.ReplaceAll(text, "[", "\\[")
	text = strings.ReplaceAll(text, "]", "\\]")
	text = strings.ReplaceAll(text, "_", "\\_")
	text = strings.ReplaceAll(text, "*", "\\*")
	text = strings.ReplaceAll(text, "`", "\\`")
	return text
}

func downloadImage(imageURL string) (string, error) {
	// Create cache directory if it doesn't exist
	cacheDir := "image_cache"
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}

	// Create a hash of the URL for a shorter, safe filename
	hasher := sha256.New()
	hasher.Write([]byte(imageURL))
	hashStr := hex.EncodeToString(hasher.Sum(nil))[:16] // Use first 16 chars of hash

	// Get the file extension from the URL path
	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(parsedURL.Path)
	if ext == "" {
		ext = ".jpg" // Default to .jpg if no extension found
	}

	// Generate filename using timestamp and hash
	filename := filepath.Join(cacheDir, fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), hashStr, ext))

	// Download the image
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: %d", resp.StatusCode)
	}

	// Create the file
	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Copy the content
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", err
	}

	return filename, nil
}

func fetchTikTokPhotos(photoURL string) ([]string, error) {
	apiURL := fmt.Sprintf("https://tikwm.com/api?url=%s&hd=1&cursor=0", url.QueryEscape(photoURL))

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tikwm API returned status: %d", resp.StatusCode)
	}

	var tikwmResp TikwmResponse
	if err := json.NewDecoder(resp.Body).Decode(&tikwmResp); err != nil {
		return nil, err
	}

	if tikwmResp.Code != 0 {
		return nil, fmt.Errorf("tikwm API error: %s", tikwmResp.Msg)
	}

	// Download all images
	var localPaths []string
	for _, imgURL := range tikwmResp.Data.Images {
		localPath, err := downloadImage(imgURL)
		if err != nil {
			log.Printf("Failed to download image %s: %v", imgURL, err)
			continue
		}
		localPaths = append(localPaths, localPath)
	}

	return localPaths, nil
}

func createURLButtons(urls []string) [][]telebot.InlineButton {
	var rows [][]telebot.InlineButton
	for i, url := range urls {
		button := telebot.InlineButton{
			Text: fmt.Sprintf("Original Link #%d", i+1),
			URL:  url,
		}
		rows = append(rows, []telebot.InlineButton{button})
	}
	return rows
}
