[![Build and Upload Artifacts](https://github.com/HeyMeco/Sanitized-TG-URL-Bot/actions/workflows/go.yml/badge.svg)](https://github.com/HeyMeco/Sanitized-TG-URL-Bot/actions/workflows/go.yml)
# Sanitized-TG-URL-Bot
Telegram bot written in Go which sanitizes URL Queries and adds x.com / tiktok links OpenGraph meta tags.
Instagram posts, carousels and reels are fetched by the bot itself and reposted as photos, videos or albums.

## Instagram
The bot loads the public post page (`instagram.com/p/<shortcode>/`) like a logged-out browser and reads the
media from the JSON Instagram embeds in it; no third-party proxy is involved. `/share/` links are resolved
to the canonical post URL. If a post can't be fetched (private, login wall, rate limit, ...) the reason is
logged and the cleaned instagram.com link is sent instead.

| Environment variable | Default | Effect |
| --- | --- | --- |
| `INSTAGRAM_LOCAL_FETCH` | `true` | Set to `false` to never download Instagram media and only clean links. |
| `INSTAGRAM_FALLBACK_HOST` | _(empty)_ | e.g. `eeinstagram.com`: rewrite post links to this host when local fetching is off or fails. |

To check parsing without Telegram: `go run ./cmd/igprobe <instagram url>...`, or
`IG_LIVE_TEST=1 go test -run TestInstagramLive -v .` (downloads into a temp dir).

Add it to your Groupchat or use it private here: [@sanitizeurlbot](https://t.me/sanitizeurlbot)

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
