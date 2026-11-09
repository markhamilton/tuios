package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Gaurav-Gosain/tuios/internal/atomicfile"
	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// What panes held on the default when the daemon last ran.
//
// A reload of config.toml applies [agents.permissions] only where it narrows
// (reloadPanePermissions). A process in a pane can still widen the file and
// then end the daemon, so the next start reads the wider file. The start
// cannot tell who started it, and the record below is a file a process of the
// user can also change, so neither is a boundary. What the start does is make
// the change visible: when the default in force at start gives panes more
// than the one recorded at the last run, it says so in the log, in
// tuios pane-grants, and in the Inbox, where the person sees it.

// appliedGrants is the record of the default in force.
type appliedGrants struct {
	Strict bool     `json:"strict"`
	Grants []string `json:"grants"`
}

// appliedGrantsPath is where the record lives, under the saved state.
func appliedGrantsPath() string {
	return filepath.Join(getResurrectionDir(), "grants", "applied.json")
}

// recordAppliedGrants writes the default now in force.
func (d *Daemon) recordAppliedGrants(r config.ResolvedPermissions) {
	data, err := json.Marshal(appliedGrants{Strict: r.Strict, Grants: r.Grants})
	if err != nil {
		return
	}
	_ = atomicfile.WritePrivate(appliedGrantsPath(), data, false)
}

// inForce is the default the grant table holds now, as a config value.
func (t *paneGrantTable) inForce() config.ResolvedPermissions {
	if !t.strict() {
		return config.ResolvedPermissions{Grants: t.strictDefaults().Names()}
	}
	return config.ResolvedPermissions{Strict: true, Grants: t.defaults().Names()}
}

// grantsWidenedNote is what the start says when panes on the default hold
// more than at the last run.
const grantsWidenedNote = "Panes on the default hold more than when tuios last ran, because config.toml changed. Check [agents.permissions] if you did not change it."

// checkGrantsSinceLastRun compares the default in force at start with the one
// recorded at the last run, says so when it widened, and records the new one.
func (d *Daemon) checkGrantsSinceLastRun() {
	now := d.manager.grants.inForce()
	if data, err := os.ReadFile(appliedGrantsPath()); err == nil {
		var prev appliedGrants
		if json.Unmarshal(data, &prev) == nil {
			was := policyDefaults(config.ResolvedPermissions{Strict: prev.Strict, Grants: prev.Grants})
			if !was.Covers(policyDefaults(now)) {
				d.grantsWidenedAtStart.Store(true)
				log.Printf("%s Before: %s. Now: %s.", grantsWidenedNote, was.String(), policyDefaults(now).String())
				d.attention.noteConfigNotice(configWidenedNotice, grantsWidenedNote)
			}
		}
	}
	d.recordAppliedGrants(now)
}

// The Inbox items about config.toml, by name.
const (
	// configWidenedNotice says panes hold more than at the last run.
	configWidenedNotice = "config.toml"
	// configWaitsNotice says a change that gives more waits for the person.
	configWaitsNotice = "config.toml change waits"
)

// configWaitsNote is the summary of the configWaitsNotice item.
const configWaitsNote = "Run tuios config apply in a terminal outside tuios. The change gives panes, other machines or a notification address more, so it waits for you."

// closeConfigNotice closes the Inbox item about config.toml named name.
func (a *attentionStore) closeConfigNotice(name string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closeKeyLocked(attentionItemKey(&AttentionItem{Kind: AttentionErrored, Name: name}), AttentionClosedResolved)
}

// noteConfigNotice opens or updates the Inbox item about config.toml named
// name, which the person dismisses.
func (a *attentionStore) noteConfigNotice(name, summary string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.upsertLocked(AttentionItem{
		Kind:    AttentionErrored,
		Name:    name,
		Summary: attentionText(summary, attentionMaxSummary),
	})
}

// verbApplyConfig applies config.toml for the person. A file change applies
// only what narrows (applyUserConfig); this is how the person applies a change
// that widens without restarting the daemon.
func (d *Daemon) verbApplyConfig(cs *connState, params json.RawMessage) (any, *verbError) {
	var p struct{}
	if verr := decodeParams(params, &p); verr != nil {
		return nil, verr
	}
	if !d.mayActAsHuman(cs) {
		return nil, hintedVerbError(ErrVerbForbidden, "apply-config is for the person: the caller runs inside a pane of this daemon", &VerbHint{
			Detail: "Nothing was applied. Run tuios config apply from a terminal outside tuios, or restart the daemon.",
		})
	}
	if d.configPath == "" {
		return nil, newVerbError(ErrVerbCommandFailed, "this daemon reads no config file")
	}
	var lc *config.LayeredConfig
	var data []byte
	lc, err := config.LoadLayered(d.configPath)
	switch {
	case err == nil:
		if data, err = lc.Bytes(); err != nil {
			return nil, newVerbError(ErrVerbCommandFailed, "config.toml could not be read: "+err.Error())
		}
	case errors.Is(err, fs.ErrNotExist):
		lc = nil
	default:
		return nil, newVerbError(ErrVerbCommandFailed, "config.toml could not be read: "+err.Error())
	}
	cfg, err := config.ParseUserConfig(data)
	if err != nil {
		return nil, newVerbError(ErrVerbCommandFailed, "config.toml has an error, so nothing was applied: "+err.Error())
	}
	// A key tuios cannot read is dropped, the same as on load, and the
	// person is told which.
	dropped := config.DroppedWarnings(config.DropUnreadableKeys(cfg, lc))

	before := d.configSnapshot()
	d.applyUserConfig(cfg, true)
	log.Printf("config.toml was applied in full by the person")
	after := d.configSnapshot()
	out := map[string]any{
		"type":           "config_applied",
		"mode":           after.mode,
		"default_grants": after.grants,
		"changes":        describeConfigChanges(before, after),
		"still_waiting":  d.configWaiting(),
		"dropped_keys":   dropped,
	}
	return out, nil
}

// configSnapshot is what apply-config reports a change of.
type configSnapshot struct {
	mode   string
	grants []string
}

func (d *Daemon) configSnapshot() configSnapshot {
	return configSnapshot{mode: d.permissionMode(), grants: d.manager.grants.defaults().Names()}
}

// describeConfigChanges says in words what changed between two snapshots.
func describeConfigChanges(before, after configSnapshot) []string {
	var out []string
	if before.mode != after.mode || !slices.Equal(before.grants, after.grants) {
		out = append(out, fmt.Sprintf("Panes on the default: mode %s, grants %s. Before: mode %s, grants %s.",
			after.mode, grantWords(after.grants), before.mode, grantWords(before.grants)))
	}
	return out
}

// grantWords is a grant list for a sentence.
func grantWords(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
