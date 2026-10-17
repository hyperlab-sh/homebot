package ui

import "testing"

func TestColorfgbgDark(t *testing.T) {
	for _, c := range []struct {
		value    string
		dark, ok bool
	}{
		{"15;0", true, true},
		{"15;6", true, true},
		{"15;8", true, true},
		{"0;7", false, true},
		{"0;9", false, true},
		{"0;15", false, true},
		{"15;default;0", true, true},
		{"0;default;15", false, true},
		{"", false, false},
		{"0", false, false},
		{"default;default", false, false},
		{"0;16", false, false},
		{"0;-1", false, false},
		{"0; 15", false, false},
		{"0;15;", false, false},
		{"1;2;3;4", false, false},
	} {
		dark, ok := colorfgbgDark(c.value)
		if dark != c.dark || ok != c.ok {
			t.Errorf("colorfgbgDark(%q) = %v, %v; want %v, %v", c.value, dark, ok, c.dark, c.ok)
		}
	}
}
