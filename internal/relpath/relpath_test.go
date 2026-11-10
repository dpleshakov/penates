package relpath

import "testing"

func TestValid(t *testing.T) {
	tests := map[string]struct {
		in   string
		want bool
	}{
		"single name":     {in: "README.md", want: true},
		"nested path":     {in: "nvim/lua/config.lua", want: true},
		"empty":           {in: "", want: false},
		"absolute path":   {in: "/etc/profile", want: false},
		"dot element":     {in: "config/./keys.lua", want: false},
		"dot-dot element": {in: "config/../secrets.lua", want: false},
		"backslash":       {in: `config\keys.lua`, want: false},
		"drive letter":    {in: "C:Users/notes.md", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := Valid(tc.in); got != tc.want {
				t.Errorf("Valid(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
