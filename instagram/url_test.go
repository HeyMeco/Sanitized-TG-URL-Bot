package instagram

import (
	"net/url"
	"testing"
)

func TestParseLink(t *testing.T) {
	tests := []struct {
		in        string
		kind      LinkKind
		shortcode string
		isReel    bool
	}{
		{"https://www.instagram.com/p/DfAWBERlz3A/", KindPost, "DfAWBERlz3A", false},
		{"https://instagram.com/p/DfAWBERlz3A", KindPost, "DfAWBERlz3A", false},
		{"https://www.instagram.com/p/De-T1-ZiKeQ/?img_index=2&igsh=abc", KindPost, "De-T1-ZiKeQ", false},
		{"https://m.instagram.com/p/-Cmh1cukG2/", KindPost, "-Cmh1cukG2", false},
		{"https://www.instagram.com/reel/Chunk8-jurw/", KindPost, "Chunk8-jurw", true},
		{"https://www.instagram.com/reels/Cop84x6u7CP/", KindPost, "Cop84x6u7CP", true},
		{"https://www.instagram.com/tv/BkfuX9UB-eK/", KindPost, "BkfuX9UB-eK", true},
		{"https://www.instagram.com/natgeo/reel/DehZj9wggt0/", KindPost, "DehZj9wggt0", true},
		{"https://www.instagram.com/natgeo/p/De-T1-ZiKeQ/", KindPost, "De-T1-ZiKeQ", false},
		{"https://WWW.INSTAGRAM.COM/p/DfAWBERlz3A/", KindPost, "DfAWBERlz3A", false},
		{"https://www.instagram.com/share/BAOfQNsBZ9/", KindShare, "", false},
		{"https://www.instagram.com/share/reel/BAOfQNsBZ9/", KindShare, "", false},
		{"https://www.instagram.com/share/p/BAKcW1XJ1s/", KindShare, "", false},
		{"https://www.instagram.com/share/", KindOther, "", false},
		{"https://www.instagram.com/reels/audio/123456789/", KindOther, "", false},
		{"https://www.instagram.com/natgeo/", KindOther, "", false},
		{"https://www.instagram.com/natgeo/profilecard/?igsh=x", KindOther, "", false},
		{"https://www.instagram.com/stories/natgeo/123/", KindOther, "", false},
		{"https://www.instagram.com/p/", KindOther, "", false},
		{"https://www.instagram.com/p/bad%20code/", KindOther, "", false},
		{"https://www.instagram.com/", KindOther, "", false},
		// Proxy and look-alike hosts are not Instagram.
		{"https://eeinstagram.com/p/DfAWBERlz3A/", KindOther, "", false},
		{"https://ddinstagram.com/reel/Chunk8-jurw/", KindOther, "", false},
		{"https://instagram.com.evil.example/p/DfAWBERlz3A/", KindOther, "", false},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.in)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", tt.in, err)
		}
		got := ParseLink(u)
		if got.Kind != tt.kind || got.Shortcode != tt.shortcode || got.IsReel != tt.isReel {
			t.Errorf("ParseLink(%q) = %+v, want kind=%d shortcode=%q isReel=%v", tt.in, got, tt.kind, tt.shortcode, tt.isReel)
		}
	}
}

func TestIsInstagramHost(t *testing.T) {
	for host, want := range map[string]bool{
		"instagram.com":     true,
		"www.instagram.com": true,
		"m.instagram.com":   true,
		"instagram.com:443": true,
		"eeinstagram.com":   false,
		"kkinstagram.com":   false,
		"instagram.co":      false,
		"":                  false,
	} {
		if got := IsInstagramHost(host); got != want {
			t.Errorf("IsInstagramHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestLinkFromRedirect(t *testing.T) {
	tests := []struct {
		in        string
		shortcode string
	}{
		{"https://www.instagram.com/reel/DehZj9wggt0/?igsh=MWQ1ZGUxMzBkMA==", "DehZj9wggt0"},
		{"https://www.instagram.com/accounts/login/?next=%2Freel%2FDehZj9wggt0%2F%3Figsh%3Dabc", "DehZj9wggt0"},
		{"https://www.instagram.com/accounts/login/?next=https%3A%2F%2Fwww.instagram.com%2Fp%2FDfAWBERlz3A%2F", "DfAWBERlz3A"},
		{"https://www.instagram.com/accounts/login/", ""},
		{"https://www.instagram.com/share/reel/BAOfQNsBZ9/", ""},
	}
	for _, tt := range tests {
		u, _ := url.Parse(tt.in)
		if got := linkFromRedirect(u); got.Shortcode != tt.shortcode {
			t.Errorf("linkFromRedirect(%q) = %+v, want shortcode %q", tt.in, got, tt.shortcode)
		}
	}
}

func TestPostURL(t *testing.T) {
	if got := PostURL("DfAWBERlz3A", false); got != "https://www.instagram.com/p/DfAWBERlz3A/" {
		t.Errorf("PostURL post = %q", got)
	}
	if got := PostURL("Chunk8-jurw", true); got != "https://www.instagram.com/reel/Chunk8-jurw/" {
		t.Errorf("PostURL reel = %q", got)
	}
}
