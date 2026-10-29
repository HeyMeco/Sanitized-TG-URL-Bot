// Command igprobe fetches Instagram posts the same way the bot does and prints
// what it found. It never talks to Telegram; use it to check live parsing.
//
//	go run ./cmd/igprobe https://www.instagram.com/p/<shortcode>/ ...
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"sanitizetelebot/instagram"
)

func main() {
	save := flag.String("save", "", "if set, write the raw post page HTML to this directory")
	flag.Parse()

	client := &instagram.Client{HTTP: &http.Client{Timeout: 20 * time.Second}}
	exit := 0
	for _, arg := range flag.Args() {
		if err := probe(client, arg, *save); err != nil {
			fmt.Printf("%s\n  ERROR: %v\n", arg, err)
			exit = 1
		}
	}
	os.Exit(exit)
}

func probe(client *instagram.Client, raw, saveDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	link := instagram.ParseLink(u)
	if link.Kind == instagram.KindShare {
		if link, err = client.ResolveShare(ctx, raw); err != nil {
			return err
		}
		fmt.Printf("%s\n  share link resolved to shortcode %s\n", raw, link.Shortcode)
	}
	if link.Kind != instagram.KindPost {
		return fmt.Errorf("not a post link")
	}
	if saveDir != "" {
		if err := savePage(ctx, link.Shortcode, saveDir); err != nil {
			return err
		}
	}

	post, err := client.FetchPost(ctx, link.Shortcode)
	if err != nil {
		return err
	}
	caption := []rune(post.Caption)
	if len(caption) > 60 {
		caption = append(caption[:60], '…')
	}
	fmt.Printf("%s\n  shortcode=%s user=%s items=%d caption=%q\n", raw, post.Shortcode, post.Username, len(post.Items), string(caption))
	for i, it := range post.Items {
		kind := "photo"
		if it.Type == instagram.Video {
			kind = "video"
		}
		host := ""
		if pu, err := url.Parse(it.URL); err == nil {
			host = pu.Host
		}
		fmt.Printf("  [%d] %s %dx%d %s\n", i+1, kind, it.Width, it.Height, host)
	}
	return nil
}

func savePage(ctx context.Context, shortcode, dir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.instagram.com/p/"+shortcode+"/", nil)
	if err != nil {
		return err
	}
	instagram.SetBrowserHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dir + "/" + shortcode + ".html")
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.ReadFrom(resp.Body)
	return err
}
