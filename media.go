package main

import (
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

// buildMediaCaption appends notes (post descriptions) to base and keeps the
// result within Telegram's caption limit, shortening the notes first.
func buildMediaCaption(base string, notes []string) string {
	if len(notes) == 0 {
		return truncateRunes(base, maxCaptionRunes)
	}
	const sep = "\n\n"
	avail := maxCaptionRunes - len([]rune(base)) - len(sep)
	if avail < 16 { // Not enough room for a useful note.
		return truncateRunes(base, maxCaptionRunes)
	}
	return base + sep + truncateRunes(strings.Join(notes, sep), avail)
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
