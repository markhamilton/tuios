package config_test

import (
	"slices"
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// TestLayoutPrefixDefaultsYieldToUserKeys is config pipeline behaviour: the
// master-stack keys (#321) became defaults under layout_prefix, and a config
// that already bound one of those keys to another action must keep it. Filling
// the default on the same key made the chord a coin flip between the two.
//
// The positive half: a key the user did not bind still gets its default.
func TestLayoutPrefixDefaultsYieldToUserKeys(t *testing.T) {
	cfg := writeConfig(t, "[keybindings]\nleader_key = \"ctrl+b\"\n\n"+
		"[keybindings.layout_prefix]\ntoggle_tiling = [\"o\"]\nrestore_all = [\"enter\"]\n")
	r := config.NewKeybindRegistry(cfg)

	for range 50 {
		if got := r.GetLayoutPrefixAction("o"); got != "toggle_tiling" {
			t.Fatalf("layout prefix o = %q, want the user's toggle_tiling", got)
		}
		if got := r.GetLayoutPrefixAction("enter"); got != "restore_all" {
			t.Fatalf("layout prefix enter = %q, want the user's restore_all", got)
		}
	}
	if keys, ok := cfg.Keybindings.LayoutPrefix["cycle_master_position"]; ok {
		t.Errorf("cycle_master_position was filled with %v over the user's o", keys)
	}
	if got := r.GetLayoutPrefixAction("m"); got != "focus_master" {
		t.Errorf("layout prefix m = %q, want the default focus_master", got)
	}
}

// TestSwapWithMasterIsOffEnter: enter is enter_terminal_mode's key, and
// swap_with_master on it read as the same key and swapped panes when the
// person meant to type into one. No preset puts it back, window-mode enter
// still enters the pane, and swap is M under the layout prefix.
//
// Negative control: restore {"enter"} as the swap_with_master default and the
// layout-prefix enter row fails for every preset.
func TestSwapWithMasterIsOffEnter(t *testing.T) {
	for _, preset := range config.Presets() {
		cfg, err := config.ParseUserConfig([]byte("[keybindings]\npreset = \"" + preset + "\"\n"))
		if err != nil {
			t.Fatalf("preset %s: %v", preset, err)
		}
		r := config.NewKeybindRegistry(cfg)
		if got := r.GetLayoutPrefixAction("enter"); got != "" {
			t.Errorf("preset %s: layout prefix enter = %q, want nothing", preset, got)
		}
		if got := r.GetLayoutPrefixAction("M"); got != "swap_with_master" {
			t.Errorf("preset %s: layout prefix M = %q, want swap_with_master", preset, got)
		}
		if got := r.GetAction("enter"); got != "enter_terminal_mode" {
			t.Errorf("preset %s: window-mode enter = %q, want enter_terminal_mode", preset, got)
		}
		for action, keys := range cfg.Keybindings.LayoutPrefix {
			if slices.Contains(keys, "enter") {
				t.Errorf("preset %s: layout prefix binds enter to %s", preset, action)
			}
		}
	}
}

// TestSwapWithMasterEnterMigrates: a config written while enter was the
// default carries swap_with_master = ["enter"], and fillMapDefaults only adds
// missing actions, so without the migration the old key would stay forever.
// A key the user chose is kept.
//
// Negative control: drop the migrateSwapWithMasterEnter call and the first
// case keeps enter.
func TestSwapWithMasterEnterMigrates(t *testing.T) {
	cases := []struct {
		written string
		want    []string
	}{
		{`["enter"]`, []string{"M"}},
		{`["enter", "x"]`, []string{"enter", "x"}},
		{`["z"]`, []string{"z"}},
	}
	for _, c := range cases {
		cfg, err := config.ParseUserConfig([]byte("[keybindings.layout_prefix]\nswap_with_master = " + c.written + "\n"))
		if err != nil {
			t.Fatalf("%s: %v", c.written, err)
		}
		if got := cfg.Keybindings.LayoutPrefix["swap_with_master"]; !slices.Equal(got, c.want) {
			t.Errorf("written %s: swap_with_master = %v, want %v", c.written, got, c.want)
		}
	}
}
