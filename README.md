[![Build and Upload Artifacts](https://github.com/HeyMeco/Sanitized-TG-URL-Bot/actions/workflows/go.yml/badge.svg)](https://github.com/HeyMeco/Sanitized-TG-URL-Bot/actions/workflows/go.yml)
# Sanitized-TG-URL-Bot
Telegram bot written in Go that removes tracking parameters from links and makes them embed nicely.
When a message contains a link it can clean, the bot reposts it as `@user said: <cleaned message>` (with
buttons to the original links) and deletes the original. Instagram posts and TikTok photo posts are
downloaded and reposted as photos, videos or albums.

Add it to your Groupchat or use it private here: [@sanitizeurlbot](https://t.me/sanitizeurlbot)

## Usage
- Send or forward a message with a link; the bot replaces it with the cleaned version.
- Include `nocut` anywhere in the message to make the bot ignore it.
- In groups, include `anon` to repost without the `@user said:` prefix.
- Inline mode: type `@sanitizeurlbot <link>` in any chat to send the cleaned link yourself.

# Run the docker image
```
docker run -d -e TELEGRAM_BOT_TOKEN=<your-token> mecoblock/sanitizetelebot
```
alternatively you can use the compose.yml:
```
services:
  sanitizetelebot:
    image: mecoblock/sanitizetelebot
    environment:
      - TELEGRAM_BOT_TOKEN=#Your token here
networks: {}
```

# To download & run the binary
1. Get a Telegram Bot Token from BotFather
2. Download the lastest artifact from the actions tab
3. Create a token.txt file and paste in your Token from Botfather
4. Run the executable
----
# To build and run it yourself
1. Get a Telegram Bot Token from BotFather
2. Clone the Repo
3. Open the Terminal in the project directory and type `go build .`
4. Create a token.txt file and paste in your Token from Botfather
5. Run the executable

On startup the bot logs `Connected to Telegram as @<bot> ... Listening for messages...` once the token
has been accepted. The token is redacted from all log output.

# Platform specifics
Common tracking parameters (`utm_*`, `fbclid`, `gclid`, `igsh`, ...) are removed from every link; see
`rules.go` for the full list.

## X / Twitter
`x.com` links are rewritten to `fixupx.com` so Telegram shows a proper preview.

## TikTok
- Short links (`vm.tiktok.com`, `pro.tiktok.com`) are expanded first.
- Video links are rewritten to `vm.dstn.to` for an embeddable preview.
- Photo posts (`/photo/`) are downloaded via tikwm.com and reposted as an album (split into parts of 10).

## Instagram
Posts, carousels (including mixed photo/video), reels and `/tv/` links are downloaded by the bot itself
and reposted as photos, videos or an album (split into parts of 10). The author and caption are shown
in an expandable quote below the link.

The bot loads the public post page (`instagram.com/p/<shortcode>/`) like a logged-out browser and reads
the media from the JSON Instagram embeds in it; no third-party proxy is involved. `/share/` links are
resolved to the canonical post URL, and all query parameters except `img_index` and `q` are dropped.

If a post can't be fetched (private account, login wall, rate limit, ...) the reason is logged and the
cleaned instagram.com link is sent instead. Stories need a logged-in account, so story links are only
cleaned.

| Environment variable | Default | Effect |
| --- | --- | --- |
| `INSTAGRAM_LOCAL_FETCH` | `true` | Set to `false` to never download Instagram media and only clean links. |
| `INSTAGRAM_FALLBACK_HOST` | _(empty)_ | e.g. `eeinstagram.com`: rewrite post links to this host when local fetching is off or fails. |

To check parsing without Telegram: `go run ./cmd/igprobe <instagram url>...`, or
`IG_LIVE_TEST=1 go test -run TestInstagramLive -v .` (downloads into a temp dir).
