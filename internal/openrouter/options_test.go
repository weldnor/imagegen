package openrouter

import "testing"

func TestNormalizeImageSize(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"1K", "1K", true},
		{"1k", "1K", true},   // the API is case-sensitive; we are not
		{" 4K ", "4K", true}, // stray whitespace from a command argument
		{"0.5K", "0.5K", true},
		{"1024x1024", "1K", true}, // legacy pixel dimensions
		{"4096X4096", "4K", true},
		{"", "", true}, // unset: model default
		{"1024", "", false},
		{"8K", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeImageSize(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeImageSize(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNormalizeAspectRatio(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"16:9", "16:9", true},
		{" 21:9 ", "21:9", true},
		{"", "", true},
		{"16x9", "", false},
		{"5:3", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeAspectRatio(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeAspectRatio(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
