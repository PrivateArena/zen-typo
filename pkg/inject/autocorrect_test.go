package inject

import (
	"testing"
)

func TestShouldIgnoreAutocorrect(t *testing.T) {
	tests := []struct {
		classes    string
		ignoreList []string
		expected   bool
	}{
		{
			classes:    "gnome-terminal-server, Gnome-terminal",
			ignoreList: []string{"terminal"},
			expected:   true,
		},
		{
			classes:    "kitty, Kitty",
			ignoreList: []string{"kitty"},
			expected:   true,
		},
		{
			classes:    "firefox, Firefox",
			ignoreList: []string{"terminal", "konsole"},
			expected:   false,
		},
		{
			classes:    "Alacritty, alacritty",
			ignoreList: []string{"alacritty"},
			expected:   true,
		},
		{
			classes:    "google-chrome, Google-chrome",
			ignoreList: []string{},
			expected:   false,
		},
	}

	for _, tc := range tests {
		got := ShouldIgnoreAutocorrect(tc.classes, tc.ignoreList)
		if got != tc.expected {
			t.Errorf("ShouldIgnoreAutocorrect(%q, %v) = %t; want %t", tc.classes, tc.ignoreList, got, tc.expected)
		}
	}
}
