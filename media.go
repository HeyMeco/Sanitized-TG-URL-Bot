package main

import (
	"html"
	"log"
	"os"
	"strings"

	tele "gopkg.in/telebot.v4"
)

// inputMedia turns a downloaded file into a Telegram album entry.
func (m mediaFile) inputMedia(caption string) tele.Inputtable {
	if m.IsVideo {
		return &tele.Video{
			File:      tele.FromDisk(m.Path),
			Width:     m.Width,
			Height:    m.Height,
			Streaming: true,
			Caption:   caption,
		}
	}
	return &tele.Photo{File: tele.FromDisk(m.Path), Caption: caption}
}

// removeMediaFiles deletes downloaded media from the cache directories.
func removeMediaFiles(files []mediaFile) {
	for _, m := range files {
		if err := os.Remove(m.Path); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove cached media %s: %v", m.Path, err)
		}
	}
}

// buildMediaCaption returns an HTML caption: base followed by notes (post
// descriptions) in an expandable blockquote. The visible text is kept within
// Telegram's caption limit, shortening the notes first.
func buildMediaCaption(base string, notes []string) string {
	const sep = "\n"
	avail := maxCaptionRunes - len([]rune(base)) - len(sep)
	if len(notes) == 0 || avail < 16 { // No notes, or not enough room for a useful one.
		return html.EscapeString(truncateRunes(base, maxCaptionRunes))
	}
	quote := truncateRunes(strings.Join(notes, "\n\n"), avail)
	return html.EscapeString(base) + sep + "<blockquote expandable>" + html.EscapeString(quote) + "</blockquote>"
}

// maxNoteCaptionRunes caps how much of a post's own caption goes into a note.
const maxNoteCaptionRunes = 300

// postNote describes a post as "<Platform> post by <author>: <caption>".
// The author is written without "@" so Telegram doesn't link it to an
// unrelated Telegram user.
func postNote(platform, author, caption string) string {
	note := platform + " post"
	if author != "" {
		note += " by " + author
	}
	if caption = strings.TrimSpace(caption); caption != "" {
		note += ": " + truncateRunes(caption, maxNoteCaptionRunes)
	}
	return note
}

// truncateRunes shortens s to at most n runes, marking the cut with an ellipsis.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return strings.TrimRight(string(r[:n-1]), " \n") + "…"
}
