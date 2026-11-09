package session

import (
	"log"
	"os"
	"path/filepath"

	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// Hot reload for config.toml.
//
// The daemon follows the config file, so a change to what it reads while it
// runs does not mean restarting the daemon that holds every running session.
//
// The watcher is only started when a config path is set, which
// DaemonConfigFromUser fills for every real starter. A daemon built from a
// hand-made DaemonConfig, which is every test, follows no file.
//
// A file that does not parse changes nothing, and the reason is logged, for
// the same reason the client keeps its running settings: a half-written file
// caught between an editor's two writes must not undo a working setting.

// startConfigWatch begins following the config file. A watcher that cannot be
// opened is logged once, and the settings then stay what they were at start.
func (d *Daemon) startConfigWatch() {
	if d.configPath == "" {
		return
	}
	// The watch is on the directory, not the file (see internal/config's
	// watcher.go), so the directory has to exist. On a machine that has never
	// saved a setting it does not.
	if err := os.MkdirAll(filepath.Dir(d.configPath), 0o750); err != nil {
		log.Printf("The daemon cannot watch the config file. A change needs a restart: %v", err)
		return
	}
	w, err := config.NewWatcherWithOptions(d.configPath, d.onConfigReload, config.WatcherOptions{
		// The settings page writes the config from a client that can share this
		// process, and its write is the change this watcher exists to see.
		DeliverSelfWrites: true,
		DeliverUnchanged:  true,
	})
	if err != nil {
		log.Printf("The daemon cannot watch the config file. A change needs a restart: %v", err)
		return
	}
	d.configWatchMu.Lock()
	d.configWatcher = w
	d.configWatchMu.Unlock()
}

// stopConfigWatch ends the config watch and returns its inotify descriptor.
func (d *Daemon) stopConfigWatch() {
	d.configWatchMu.Lock()
	w := d.configWatcher
	d.configWatcher = nil
	d.configWatchMu.Unlock()
	if w != nil {
		w.Stop()
	}
}

// onConfigReload runs on the watcher goroutine. It applies [notify],
// appearance.preferred_shell, workspaces.return_when_empty,
// [paste_buffers], [agents] enabled and herdr_protocol, [daemon]
// single_client and ssh_agent, the
// [agents.approvals], [agents.permissions] and [agents.queue] tables and
// [agents.recap] test_patterns, and reads nothing else out of the file. A new
// approval policy applies to the next request, except a risk rule it drops or
// a panes_may_allow it sets, which wait (narrowApprovalPolicy); a hold
// already running keeps the length it started with. A new permission default that narrows applies
// to the next call from every pane that holds the default; one that widens
// waits for a restart (reloadPanePermissions).
func (d *Daemon) onConfigReload(cfg *config.UserConfig, err error) {
	if err != nil {
		log.Printf("The config file has an error, so the daemon's settings did not change: %v", err)
		return
	}
	d.applyUserConfig(cfg, false)
}

// applyUserConfig applies what the daemon reads from config.toml while it
// runs. The file is the user's, and a process in a pane runs as the user and
// can write it. So from a file change (byPerson false) the parts that bound
// panes apply only where they narrow: [agents.permissions] and the risk rules
// of [agents.approvals]. What widens waits for tuios config apply from outside
// every pane (byPerson true), or a daemon restart.
func (d *Daemon) applyUserConfig(cfg *config.UserConfig, byPerson bool) {
	// The agent switch applies from a file change too, in both directions.
	// Off is the stricter state (see agents_switch.go), and on is the state
	// every daemon runs in by default, so neither widens what a pane may do
	// beyond what the shipped daemon allows.
	d.SetAgentsEnabled(cfg.Agents.On())
	// It narrows who is attached and widens nothing, so a file change
	// applies it at once.
	d.singleClient.Store(cfg.Daemon.SingleClient)
	// The link only ever names a socket of the person's own client, so
	// following it widens nothing a pane may do, and a file change applies.
	d.SetSSHAgent(cfg.Daemon.SSHAgent)
	// The display only ever comes from the person's own local client, so a
	// file change applies at once, like the agent.
	d.SetDisplay(cfg.Daemon.Display)
	d.manager.SetPreferredShell(cfg.Appearance.PreferredShell)
	d.manager.SetHerdrProtocol(cfg.Agents.HerdrProtocol)
	d.manager.SetReturnWhenEmpty(cfg.Workspaces.ReturnsWhenEmpty())
	d.manager.SetDragAndDrop(cfg.Panes.DragAndDrop)
	// Where a new worktree goes widens nothing a pane may do: a pane that can
	// write the file can already run git worktree add itself.
	d.SetWorktreePath(cfg.Worktree.Path)
	if d.buffers != nil {
		d.buffers.SetLimits(cfg.PasteBuffers.Resolved())
	}
	// The risk rules and panes_may_allow bound what a pane may allow, so a
	// file change only narrows them, like [agents.permissions].
	approvals, approvalsWait := ApprovalPolicyFromConfig(cfg.Agents.Approvals), false
	if !byPerson {
		approvals, approvalsWait = narrowApprovalPolicy(d.approvalPolicy(), approvals)
		if approvalsWait {
			log.Printf("The new [agents.approvals.risk] gives panes more than before. The risk rules in force stay, and panes may not allow a risky call. The rest applies after tuios config apply from outside tuios, or a daemon restart.")
		}
	}
	d.SetApprovalPolicy(approvals)
	d.approvalsWaiting.Store(approvalsWait)
	d.SetRecapTestPatterns(cfg.Agents.Recap.Resolved().TestPatterns)
	d.SetQueueMax(cfg.Agents.Queue.MaxEntries())
	// It changes only what a pane is shown as, so a file change applies.
	d.SetAgentSilentAfter(resolveSilentAfter(silentAfterFromConfig(cfg.Agents)))
	// A file change only turns the wake off. A process in a pane can write
	// the file, and the wake types into panes, so turning it on waits for
	// tuios config apply from outside tuios, or a daemon restart, as a wider
	// [agents.permissions] does.
	wake := cfg.Agents.Mail.WakeOn()
	if wake && !byPerson && !d.mailWake.on.Load() {
		wake = false
		log.Printf("[agents.mail] wake = true in the file waits for tuios config apply from outside tuios, or a daemon restart. The wake stays off.")
	}
	d.SetMailWake(wake)
	d.SetReviewMaxUntracked(cfg.Review)
	// Moving cards types nothing and runs nothing, so a file change applies
	// either way.
	d.taskAutoOff.Store(!cfg.Tasks.AutoStageOn())
	// A new notification destination waits for the person.
	d.notify.reload(cfg.Notify, byPerson)
	perms := PanePermissionsFromConfig(cfg.Agents.Permissions)
	// A plugin put on the enabled list waits for the person. One taken off
	// stops now. See plugin_host.go.
	pluginsWait := d.plugins != nil && d.plugins.apply(cfg.Plugins, byPerson)
	d.pluginsWaiting.Store(pluginsWait)
	if byPerson {
		d.manager.SetPanePermissions(perms)
		d.recordAppliedGrants(perms)
		d.noteConfigWaiting()
		return
	}
	d.reloadPanePermissions(perms)
	d.noteConfigWaiting()
}

// noteConfigWaiting opens the Inbox item that says a change waits for the
// person, or closes it when nothing waits.
func (d *Daemon) noteConfigWaiting() {
	if d.configWaiting() {
		d.attention.noteConfigNotice(configWaitsNotice, configWaitsNote)
		return
	}
	d.attention.closeConfigNotice(configWaitsNotice)
}

// configWaiting reports whether config.toml holds a change that widens what
// panes may do and waits for tuios config apply or a restart.
func (d *Daemon) configWaiting() bool {
	return d.manager.grants.restartNeeded.Load() || d.notify.waitingApply() || d.pluginsWaiting.Load() || d.approvalsWaiting.Load()
}
