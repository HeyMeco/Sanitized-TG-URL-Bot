[![Build and Upload Artifacts](https://github.com/HeyMeco/Sanitized-TG-URL-Bot/actions/workflows/go.yml/badge.svg)](https://github.com/HeyMeco/Sanitized-TG-URL-Bot/actions/workflows/go.yml)
# Sanitized-TG-URL-Bot
Telegram bot written in Go which sanitizes URL Queries and adds x.com / tiktok links OpenGraph meta tags

Add it to your Groupchat or use it private here: [@sanitizeurlbot](https://t.me/sanitizeurlbot)

# Run the docker image
```
docker run -d -e TELEGRAM_BOT_TOKEN=<your-token> mecoblock/sanitizetelebot
```
alternatively you can use the compose.yml:
```
version: "3.3"
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
3. Open the Terminal in the project directory and type `go build`
4. Create a token.txt file and paste in your Token from Botfather
5. Run the executable
