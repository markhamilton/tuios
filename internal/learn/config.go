package learn

import "github.com/Gaurav-Gosain/tuios/internal/config"

// Config is the configuration the tour runs with: the shipped defaults, so
// the tour shows the screen a person sees after installing tuios. The
// lessons are worded for that screen: the dock and the window titles at the
// top, the rail on the right. Each lesson starts with the reset command,
// which closes the rail, so a step that opens it opens it.
func Config() *config.UserConfig {
	cfg := config.DefaultConfig()
	// The lessons are worded for the tuios keys, which DefaultConfig holds.
	// Named, because an empty preset is config.DefaultPreset (tmux).
	cfg.Keybindings.Preset = config.PresetTuios
	// A browser tab has no desktop notifications. Left on, the default warns
	// about it at startup, which is noise in a tutorial.
	cfg.Notifications.Agent.Notify = new(false)
	// The lessons teach window mode and the i that leaves it, step by step
	// after each new window, so they keep upstream's mode handling rather
	// than the fork's terminal home mode.
	cfg.Appearance.ReturnToWindowMode = new(true)
	return cfg
}
