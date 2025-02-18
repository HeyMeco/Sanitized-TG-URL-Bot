// v2 uses the rules from https://github.com/Vendicated/Vencord/tree/main/src/plugins/clearURLs so credits to them
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

			// Create query parameter rules based on defaultRules.ts
			universalRules := []string{
				"action_object_map",
				"action_type_map",
				"action_ref_map",
				"spm@*.aliexpress.com",
				"scm@*.aliexpress.com",
				"aff_platform",
				"aff_trace_key",
				"algo_expid@*.aliexpress.*",
				"algo_pvid@*.aliexpress.*",
				"btsid",
				"ws_ab_test",
				"pd_rd_*@amazon.*",
				"_encoding@amazon.*",
				"psc@amazon.*",
				"tag@amazon.*",
				"ref_@amazon.*",
				"pf_rd_*@amazon.*",
				"pf@amazon.*",
				"crid@amazon.*",
				"keywords@amazon.*",
				"sprefix@amazon.*",
				"sr@amazon.*",
				"ie@amazon.*",
				"node@amazon.*",
				"qid@amazon.*",
				"callback@bilibili.com",
				"cvid@bing.com",
				"form@bing.com",
				"sk@bing.com",
				"sp@bing.com",
				"sc@bing.com",
				"qs@bing.com",
				"pq@bing.com",
				"sc_cid",
				"mkt_tok",
				"trk",
				"trkCampaign",
				"ga_*",
				"gclid",
				"gclsrc",
				"hmb_campaign",
				"hmb_medium",
				"hmb_source",
				"spReportId",
				"spJobID",
				"spUserID",
				"spMailingID",
				"itm_*",
				"s_cid",
				"elqTrackId",
				"elqTrack",
				"assetType",
				"assetId",
				"recipientId",
				"campaignId",
				"siteId",
				"mc_cid",
				"mc_eid",
				"pk_*",
				"sc_campaign",
				"sc_channel",
				"sc_content",
				"sc_medium",
				"sc_outcome",
				"sc_geo",
				"sc_country",
				"nr_email_referer",
				"vero_conv",
				"vero_id",
				"yclid",
				"_openstat",
				"mbid",
				"cmpid",
				"cid",
				"c_id",
				"campaign_id",
				"Campaign",
				"hash@ebay.*",
				"fb_action_ids",
				"fb_action_types",
				"fb_ref",
				"fb_source",
				"fbclid",
				"refsrc@facebook.com",
				"hrc@facebook.com",
				"gs_l",
				"gs_lcp@google.*",
				"ved@google.*",
				"ei@google.*",
				"sei@google.*",
				"gws_rd@google.*",
				"gs_gbg@google.*",
				"gs_mss@google.*",
				"gs_rn@google.*",
				"_hsenc",
				"_hsmi",
				"__hssc",
				"__hstc",
				"hsCtaTracking",
				"source@sourceforge.net",
				"position@sourceforge.net",
				"t@*.twitter.com",
				"s@*.twitter.com",
				"ref_*@*.twitter.com",
				"t@*.x.com",
				"s@*.x.com",
				"ref_*@*.x.com",
				"t@*.fixupx.com",
				"s@*.fixupx.com",
				"ref_*@*.fixupx.com",
				"t@*.fxtwitter.com",
				"s@*.fxtwitter.com",
				"ref_*@*.fxtwitter.com",
				"t@*.twittpr.com",
				"s@*.twittpr.com",
				"ref_*@*.twittpr.com",
				"t@*.fixvx.com",
				"s@*.fixvx.com",
				"ref_*@*.fixvx.com",
				"tt_medium",
				"tt_content",
				"lr@yandex.*",
				"redircnt@yandex.*",
				"feature@*.youtube.com",
				"kw@*.youtube.com",
				"si@*.youtube.com",
				"pp@*.youtube.com",
				"si@*.youtu.be",
				"wt_zmc",
				"utm_source",
				"utm_content",
				"utm_medium",
				"utm_campaign",
				"utm_term",
				"si@open.spotify.com",
				"igshid",
				"igsh",
				"share_id@reddit.com",
				"si@soundcloud.com",
			}

			// Host-specific rules
			hostRules := map[string][]string{
				"amazon":         {"pd_rd_", "_encoding", "psc", "tag", "ref_", "pf_rd_", "pf", "crid"},
				"youtube.com":    {"feature", "kw", "si", "pp"},
				"youtu.be":       {"si"},
				"twitter.com":    {"t", "s", "ref_"},
				"x.com":          {"t", "s", "ref_"},
				"instagram.com":  {"igshid"},
				"spotify.com":    {"si"},
				"reddit.com":     {"share_id"},
				"soundcloud.com": {"si"},
				"tiktok":         {"_r", "_t"},
			}

			// Clean universal parameters
			q := parsedURL.Query()
			for param := range q {
				for _, rule := range universalRules {
					if strings.HasPrefix(param, rule) {
						q.Del(param)
						sanitized = true
					}
				}
			}

			// Clean host-specific parameters
			for host, rules := range hostRules {
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
				parsedURL.Host = "ddinstagram.com"
				sanitized = true

				// Logic to remove "profilecard" path as those result in an error page without share id
				pathSegments := strings.Split(parsedURL.Path, "/")
				if len(pathSegments) > 2 && pathSegments[2] == "profilecard" {
					// Reconstruct the path without the "profilecard" segment
					parsedURL.Path = "/" + pathSegments[1]
					sanitized = true
				}
			}

			sanitizedWords = append(sanitizedWords, parsedURL.String())
		} else {
			sanitizedWords = append(sanitizedWords, word)
		}
	}

	return strings.Join(sanitizedWords, " "), sanitized, nil
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
