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
	file, err := os.Open("token.txt")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer file.Close()

	token, err := io.ReadAll(file)
	if err != nil {
		log.Fatal(err)
		return
	}

	b, err := telebot.NewBot(telebot.Settings{
		Token:  string(token),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})

	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle(telebot.OnText, func(m *telebot.Message) {
		username := getUsername(m.Sender)

		sanitizedMsg, sanitized, err := sanitizeURL(m.Text)
		if err != nil {
			log.Println(err)
			return
		}

		if sanitized {
			b.Send(m.Chat, "@"+username+" said: "+sanitizedMsg)
			b.Delete(m)
		}
	})

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

	if parsedURL.RawQuery != "" || parsedURL.Host == "x.com" || strings.HasSuffix(parsedURL.Host, "tiktok.com") {
		parsedURL.RawQuery = ""

		if strings.HasSuffix(parsedURL.Host, "tiktok.com") {
			parsedURL.Host = "vm.dstn.to"
		}
		if parsedURL.Host == "x.com" {
			parsedURL.Host = "fixupx.com"
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
