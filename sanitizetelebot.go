package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	telebot "gopkg.in/tucnak/telebot.v2"
)

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

		sanitizedMsg, sanitized, err := sanitizeURL(m.Text)
		if err != nil {
			log.Println(err)
			return
		}

		if sanitized {
			if m.FromGroup() && strings.Contains(m.Text, "anon") {
				b.Send(m.Chat, strings.Replace(sanitizedMsg, "anon", "", 1))
			} else {
				b.Send(m.Chat, "@"+username+" said: "+sanitizedMsg)
			}
			b.Delete(m)
		}
	})

	b.Handle(telebot.OnQuery, func(q *telebot.Query) {
		sanitizedMsg, sanitized, err := sanitizeURL(q.Text)
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

func sanitizeURL(text string) (string, bool, error) {
	words := strings.Fields(text)
	var sanitizedWords []string
	var sanitized bool

	for _, word := range words {
		if containsURL(word) {
			parsedURL, err := url.Parse(word)
			if err != nil {
				sanitizedWords = append(sanitizedWords, word)
				continue
			}

			if parsedURL.Host == "vm.tiktok.com" || parsedURL.Host == "tiktok.com" {
				word, err = ExpandUrl(word)
				if err != nil {
					return "", false, err
				}
				parsedURL, err = url.Parse(word)
				if err != nil {
					return "", false, err
				}
			}

			word, urlSanitized := sanitizeParsedURL(parsedURL)
			if urlSanitized {
				sanitized = true
			}
			sanitizedWords = append(sanitizedWords, word)
		} else {
			sanitizedWords = append(sanitizedWords, word)
		}
	}

	return strings.Join(sanitizedWords, " "), sanitized, nil
}

func containsURL(text string) bool {
	return strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")
}

func sanitizeParsedURL(parsedURL *url.URL) (string, bool) {
	var sanitized bool

	if parsedURL.RawQuery != "" || parsedURL.Host == "x.com" || strings.HasSuffix(parsedURL.Host, "instagram.com") || strings.HasSuffix(parsedURL.Host, "tiktok.com") {

		if strings.HasSuffix(parsedURL.Host, "youtube.com") {
			// Extract the video ID from the URL
			videoID := parsedURL.Query().Get("v")
			if videoID != "" {
				// Reconstruct the URL with only the video ID
				return fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID), false
			}
		}

		parsedURL.RawQuery = ""

		if strings.HasSuffix(parsedURL.Host, "tiktok.com") {
			// Check if the expanded URL contains "/photo/"
			if !strings.Contains(parsedURL.Path, "/photo/") {
				parsedURL.Host = "vm.dstn.to"
			}
		}
		if parsedURL.Host == "x.com" {
			parsedURL.Host = "fixupx.com"
		}
		if strings.HasSuffix(parsedURL.Host, "instagram.com") {
			parsedURL.Host = "ddinstagram.com"
		}

		sanitized = true
	}

	return parsedURL.String(), sanitized
}

func ExpandUrl(shortURL string) (string, error) {
	resp, err := http.Head(shortURL)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Received non-200 status code")
	}
	return resp.Request.URL.String(), nil
}
