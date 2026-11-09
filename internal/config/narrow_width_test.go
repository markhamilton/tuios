package config

import "testing"

// TestNarrowWidthOption: appearance.narrow_width defaults to 100, an explicit
// 0 survives (narrow view off), a value past the range is clamped, and the
// option is in the registry the settings page and set-config read.
func TestNarrowWidthOption(t *testing.T) {
	for _, tc := range []struct {
		file string
		want int
	}{
		{"", NarrowWidthDefault},
		{"[appearance]\nnarrow_width = 0\n", 0},
		{"[appearance]\nnarrow_width = 120\n", 120},
		{"[appearance]\nnarrow_width = 5000\n", NarrowWidthMax},
		{"[appearance]\nnarrow_width = -3\n", 0},
	} {
		cfg, err := ParseUserConfig([]byte(tc.file))
		if err != nil {
			t.Fatalf("%q: %v", tc.file, err)
		}
		s := DefaultSettings()
		ApplyAppearanceConfig(cfg, &s)
		if s.NarrowWidth != tc.want {
			t.Errorf("%q: NarrowWidth = %d, want %d", tc.file, s.NarrowWidth, tc.want)
		}
	}
	if DefaultSettings().NarrowWidth != NarrowWidthDefault {
		t.Errorf("DefaultSettings().NarrowWidth = %d, want %d", DefaultSettings().NarrowWidth, NarrowWidthDefault)
	}
	found := false
	for _, opt := range Options() {
		if opt.Path == "appearance.narrow_width" {
			found = true
			if opt.Type != OptionInt || opt.Min != 0 || opt.Max != NarrowWidthMax {
				t.Errorf("the option is %+v", opt)
			}
		}
	}
	if !found {
		t.Error("appearance.narrow_width is not in the option registry")
	}
}
