package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestRedactingWriter(t *testing.T) {
	const token = "123456:FAKE-test_token"
	var buf bytes.Buffer
	logger := log.New(&redactingWriter{w: &buf, secret: token}, "", 0)

	err := errors.New(`telebot: Post "https://api.telegram.org/bot` + token + `/getMe": dial tcp: i/o timeout`)
	logger.Printf("Failed to create bot: %v", err)
	logger.Println("no secret here")

	out := buf.String()
	if strings.Contains(out, token) {
		t.Fatalf("token leaked: %q", out)
	}
	if !strings.Contains(out, "/bot<redacted>/getMe") || !strings.Contains(out, "no secret here") {
		t.Errorf("unexpected output: %q", out)
	}
}
