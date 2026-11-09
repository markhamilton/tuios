package session

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/Gaurav-Gosain/tuios/internal/pathroot"
	"golang.org/x/text/unicode/norm"
)

// Where a file read or write that arrives over a link may land.
//
// The file verbs act on this machine for whoever calls them. The person's own
// clients may read and write anywhere the user may. A call that arrived over a
// link comes from another machine, and the link policy (link_policy.go) only
// says whether that machine may use the file verbs at all. A sender that lies,
// or a copy aimed at the wrong folder, must not be able to drop a key into
// ~/.ssh/authorized_keys or a line into ~/.bashrc: that is how a file copy
// becomes a login (croc CVE-2023-43619 is that bug). Nor may it read the keys
// out.
//
// So a write over a link (open-file-stream write, file-commit, file-mkdir,
// file-rename, file-remove, file-abort) is held to three rules:
//
//   - It lands inside one of the write roots: the folders files_roots names
//     for the machine, or the receive folder (config.DefaultFilesRoot) when
//     files_roots says nothing, plus this daemon's drop folder. The check is
//     on the real path, after links, and the write itself goes through an
//     os.Root at that folder, so a link inside the folder cannot carry the
//     write out of it.
//   - It does not land in the deny list below, nor does the deny list sit
//     under it: the folders that hold keys, credentials, shell start files,
//     login items, and tuios's own config and state. A write to a folder that
//     holds one of them, such as a rename of ~/.config, is refused too, so a
//     link cannot carry a denied folder out and back. The check is on the
//     real path, and it folds case and width the way macOS and Windows disks
//     do.
//   - The deny list holds inside every root, so files_roots = ["~"] still
//     keeps a write out of ~/.ssh.
//
// A read over a link (file-read, file-hash, file-walk, open-file-stream read)
// is held to the read deny list: keys, credentials, shell start files, /proc,
// the daemon's socket folder and tuios's own state. Every credential carrier of
// the write deny list is in it (linkDenyCredentialHome). A read elsewhere is allowed, so a copy
// out works. A read does not go through a root, so on Windows a path must also
// be a plain drive letter path (plainLinkPath).
//
// The deny check reads the real path before the call. A process on this
// machine that swaps a folder for a link between the check and the write could
// still steer a write within a root, but such a process already runs as the
// user and needs no link to do that. The link itself cannot make a link: no
// verb creates one, a copy skips them, and a rename of a link whose target is
// denied, or holds a denied folder, is refused. What is left is a link that
// was already inside a root, pointing at a denied place that is also inside
// it (files_roots = ["~"]), inside a folder the link renames between another
// call's check and its open. That needs a root the person widened on purpose,
// and a link the person made.

// linkDenyCredentialHome is the credential carriers, as paths under the home
// folder: keys, tokens, the files a shell or a session reads its environment
// from, and tuios's own config and state. Both deny lists hold all of them,
// so a file a link may not change because it can hold a secret may not be
// read either. A shell start file or ~/.gitconfig often exports a token, and
// ~/.config/git holds the credential store git writes by default.
var linkDenyCredentialHome = []string{
	".ssh",
	".gnupg",
	".config/tuios",
	".local/state/tuios",
	".local/share/tuios",
	"Library/Application Support/tuios",
	".config/systemd",
	".config/environment.d",
	".config/fish",
	".config/git",
	".config/zsh",
	".config/gcloud",
	".config/tuios-web",
	".git-credentials",
	".gitconfig",
	".netrc",
	".aws",
	".azure",
	".kube",
	".password-store",
	".pam_environment",
	".profile",
	".bash_profile",
	".bash_login",
	".bash_logout",
	".bashrc",
	".bashrc.d",
	".zshenv",
	".zprofile",
	".zshrc",
	".zlogin",
	".zlogout",
	".cshrc",
	".tcshrc",
	".kshrc",
	".mkshrc",
	".xprofile",
	".xsession",
	".xinitrc",
	".inputrc",
	// The PowerShell profiles, which are shell start files too.
	"Documents/PowerShell",
	"Documents/WindowsPowerShell",
	// Claude Code's login and the MCP servers it runs.
	".claude.json",
	// The sops age keys, which decrypt every file sops encrypted.
	".config/sops/age",
	"Library/Application Support/sops/age",
	"AppData/Roaming/sops/age",
	// Atuin's sync key, session and history, and the config that names
	// their paths and the sync server.
	".local/share/atuin",
	".config/atuin",
	// Ollama's signing key, and the API keys of Gemini CLI, its fork Qwen
	// Code, and Hermes Agent.
	".ollama/id_ed25519",
	".gemini/.env",
	".qwen/.env",
	".hermes/.env",
	"AppData/Local/hermes/.env",
}

// linkDenyWriteOnlyHome is the rest of the write deny list: login items,
// which run a program but hold no secret, the folders of tools whose
// credential file the read deny list names on its own (~/.claude,
// ~/.codex, ~/.docker, ~/.config/gh), and the config folders of the agent
// harnesses tuios integrates (internal/integration), which name the hooks,
// plugins, extensions and MCP servers the tool runs. A link may read the
// rest of those folders, and may change none of them.
var linkDenyWriteOnlyHome = []string{
	".config/autostart",
	"Library/LaunchAgents",
	"Library/LaunchDaemons",
	".docker",
	".claude",
	".codex",
	".config/gh",
	// The agent harnesses tuios installs hooks, plugins or extensions into.
	".gemini",
	".config/opencode",
	".config/amp",
	".copilot",
	".config/crush",
	".cursor",
	".config/devin",
	"AppData/Roaming/devin",
	".factory",
	".grok",
	".hermes",
	"AppData/Local/hermes",
	".config/kilo",
	".kimi-code",
	".pi/agent",
	".omp/agent",
	".qoder",
	".qwen",
	// The Windows start folder.
	"AppData/Roaming/Microsoft/Windows/Start Menu/Programs/Startup",
}

// linkDenyHome is the write deny list, as paths under the home folder. A write
// to one of them, to anything under it, or to a folder that holds it, is
// refused.
var linkDenyHome = slices.Concat(linkDenyCredentialHome, linkDenyWriteOnlyHome)

// linkDenyAbsolute is the write deny list outside the home folder: the crontab
// spools. They are outside every root a link may write, so they are refused
// twice, which is the point.
var linkDenyAbsolute = []string{
	"/var/spool/cron",
	"/var/at/tabs",
	"/var/cron/tabs",
	"/proc",
}

// linkReadDenyAbsolute is the read deny list outside the home folder. /proc
// holds every process's environment and memory as files a plain read returns:
// /proc/self/environ and /proc/self/mem are the daemon's own, and the panes'
// shells are its children.
var linkReadDenyAbsolute = []string{
	"/proc",
}

// linkReadDenyHome is the read deny list, as paths under the home folder. A
// read of one of these, over a link, is refused, with an exception for the
// public keys in ~/.ssh. It holds every credential carrier of the write deny
// list (linkDenyCredentialHome), and the stores, histories and browser
// profiles below, which a link may not read but which a write needs no rule
// for.
var linkReadDenyHome = slices.Concat(linkDenyCredentialHome, linkReadDenyOnlyHome)

// linkReadDenyOnlyHome is the part of the read deny list that is not in the
// write deny list.
var linkReadDenyOnlyHome = []string{
	".docker/config.json",
	".config/gh/hosts.yml",
	"Library/Keychains",
	".claude/.credentials.json",
	".config/claude/.credentials.json",
	".codex/auth.json",
	".npmrc",
	".pypirc",
	".gem/credentials",
	".cargo/credentials",
	".cargo/credentials.toml",
	".vault-token",
	".pgpass",
	".my.cnf",
	".terraform.d/credentials.tfrc.json",
	".config/rclone",
	".config/hub",
	".config/op",
	".local/share/keyrings",
	// Shell and program histories: a command line often holds a token.
	".bash_history",
	".zsh_history",
	".zhistory",
	".histfile",
	".sh_history",
	".local/share/fish/fish_history",
	".python_history",
	".node_repl_history",
	".psql_history",
	".mysql_history",
	".sqlite_history",
	".lesshst",
	".viminfo",
	".local/state/nvim/shada",
	".local/share/zsh/history",
	".local/state/zsh/history",
	// Browser profiles: cookies are signed-in sessions.
	".mozilla",
	".config/google-chrome",
	".config/chromium",
	".config/BraveSoftware",
	".config/microsoft-edge",
	"Library/Application Support/Google/Chrome",
	"Library/Application Support/Firefox",
	"Library/Application Support/BraveSoftware",
	"Library/Cookies",
	"Library/Safari",
	// Browsers and mail installed as snaps and flatpaks keep their
	// profiles here: the snap's own home, and a flatpak's whole data.
	"snap/firefox/common/.mozilla",
	"snap/chromium/common/chromium",
	"snap/thunderbird/common/.thunderbird",
	".var/app",
	// Mail, chat and editor state: signed-in sessions and tokens.
	".thunderbird",
	"Library/Thunderbird",
	".config/Code",
	"Library/Application Support/Code",
	".config/Cursor",
	"Library/Application Support/Cursor",
	"AppData/Roaming/Cursor",
	".config/Signal",
	"Library/Application Support/Signal",
	".config/discord",
	"Library/Application Support/discord",
	".config/Slack",
	"Library/Application Support/Slack",
	".local/share/TelegramDesktop",
	// Crypto wallets.
	".bitcoin",
	".electrum",
	".ethereum",
	".monero",
	".litecoin",
	".config/solana",
	".config/Exodus",
	"Library/Application Support/Bitcoin",
	"Library/Application Support/Exodus",
	"Library/Application Support/Electrum",
	"AppData/Roaming/Bitcoin",
	"AppData/Roaming/Electrum",
	"AppData/Roaming/Exodus",
	// Password managers and the tokens of other tools.
	".config/Bitwarden",
	".config/1Password",
	".config/github-copilot",
	// The logins of agent harnesses, and Cursor's MCP servers, whose env
	// often holds API keys.
	".gemini/oauth_creds.json",
	".gemini/mcp-oauth-tokens.json",
	".local/share/opencode/auth.json",
	".local/share/opencode/mcp-auth.json",
	".qwen/oauth_creds.json",
	".local/share/kilo/auth.json",
	".local/share/kilo/mcp-auth.json",
	".pi/agent/auth.json",
	// oh-my-pi, a Pi fork, keeps its logins in a SQLite database, and the
	// write-ahead log beside it holds the newest of them.
	".omp/agent/agent.db",
	".omp/agent/agent.db-wal",
	".local/share/amp/secrets.json",
	".copilot/config.json",
	".cursor/mcp.json",
	// Cloud and tunnel CLI credentials.
	".oci",
	".config/doctl",
	"Library/Application Support/doctl",
	"AppData/Roaming/doctl",
	".config/ngrok",
	"Library/Application Support/ngrok",
	"AppData/Local/ngrok",
	".ngrok2",
	// Windows credentials and the DPAPI keys that open them.
	"AppData/Roaming/Microsoft/Credentials",
	"AppData/Local/Microsoft/Credentials",
	"AppData/Roaming/Microsoft/Protect",
}

// fileFS is where the file verbs write. A zero fileFS is the plain file
// system, for the person's own calls. With root set, every path must be under
// dir, the real path the root was opened at, and every operation goes
// through the root.
type fileFS struct {
	root *os.Root
	dir  string
	// link says the write came from another machine: a linked machine or a
	// paired device. A file it commits gets the sender's mode the way cp and
	// scp without -p give it, under this process's umask, and never with
	// group or world write. See commitMode.
	link bool
}

// commitMode is the mode a committed file gets for the sender's perm. The
// person's own copy, and a pane's, keep the bits as sent, as the docs
// promise. A copy from another machine is masked by the umask and loses
// group and world write, so a file another local user could change never
// arrives that way on a shared machine. The execute bits stay, so a script
// that arrives still runs.
func (f fileFS) commitMode(perm uint32) os.FileMode {
	m := os.FileMode(perm & 0o777)
	if f.link {
		m &^= os.FileMode(processUmask) | 0o022
	}
	return m
}

// rel is path relative to the root.
func (f fileFS) rel(path string) (string, error) {
	r, ok := pathroot.Within(f.dir, path)
	if !ok {
		return "", &fs.PathError{Op: "write", Path: path, Err: fs.ErrPermission}
	}
	return r, nil
}

func (f fileFS) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	if f.root == nil {
		return os.OpenFile(path, flag, perm)
	}
	r, err := f.rel(path)
	if err != nil {
		return nil, err
	}
	return f.root.OpenFile(r, flag, perm)
}

func (f fileFS) Lstat(path string) (fs.FileInfo, error) {
	if f.root == nil {
		return os.Lstat(path)
	}
	r, err := f.rel(path)
	if err != nil {
		return nil, err
	}
	return f.root.Lstat(r)
}

func (f fileFS) Stat(path string) (fs.FileInfo, error) {
	if f.root == nil {
		return os.Stat(path)
	}
	r, err := f.rel(path)
	if err != nil {
		return nil, err
	}
	return f.root.Stat(r)
}

func (f fileFS) Chmod(path string, mode os.FileMode) error {
	if f.root == nil {
		return os.Chmod(path, mode)
	}
	r, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.Chmod(r, mode)
}

func (f fileFS) Chtimes(path string, at, mt time.Time) error {
	if f.root == nil {
		return os.Chtimes(path, at, mt)
	}
	r, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.Chtimes(r, at, mt)
}

func (f fileFS) Rename(from, to string) error {
	if f.root == nil {
		return os.Rename(from, to)
	}
	a, err := f.rel(from)
	if err != nil {
		return err
	}
	b, err := f.rel(to)
	if err != nil {
		return err
	}
	return f.root.Rename(a, b)
}

func (f fileFS) Remove(path string) error {
	if f.root == nil {
		return os.Remove(path)
	}
	r, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.Remove(r)
}

func (f fileFS) RemoveAll(path string) error {
	if f.root == nil {
		return os.RemoveAll(path)
	}
	r, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.RemoveAll(r)
}

func (f fileFS) MkdirAll(path string, perm os.FileMode) error {
	if f.root == nil {
		return os.MkdirAll(path, perm)
	}
	r, err := f.rel(path)
	if err != nil {
		return err
	}
	if r == "." {
		return nil
	}
	return f.root.MkdirAll(r, perm)
}

// Close releases the root. A file opened through it stays open.
//
//go:noinline
func (f fileFS) Close() {
	if f.root != nil {
		_ = f.root.Close()
	}
}

// foldPaths reports whether this system's disks ignore case by default.
var foldPaths = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// foldKey is p with each component folded the way a case-insensitive disk
// compares it: normalised to NFD, then each rune mapped to the smallest in its
// Unicode simple-fold cycle, and the ignorable code points a disk drops
// removed. It is used only to compare two paths, never to open one. Without it
// ".Ssh", ".ſsh" (long s) and a soft hyphen in ".s<ad>sh" each open ".ssh" on
// APFS while strings.ToLower leaves them apart.
func foldKey(p string) string {
	if runtime.GOOS == "windows" {
		p = trimWindowsNames(p)
	}
	p = norm.NFD.String(p)
	var b strings.Builder
	b.Grow(len(p))
	for _, r := range p {
		switch r {
		case 0x00AD, 0xFEFF, 0x200B, 0x200C, 0x200D:
			// Soft hyphen, BOM, zero-width space and the zero-width
			// non-joiner and joiner, as numbers so the source holds no
			// invisible character: a disk drops them.
			continue
		}
		b.WriteRune(foldRune(r))
	}
	return b.String()
}

// trimWindowsNames is p with the dots and spaces at the end of each name
// removed. Windows drops them when it opens a path, and so does os.Root, so
// ".ssh." and ".ssh " open ".ssh".
func trimWindowsNames(p string) string {
	parts := strings.Split(p, string(filepath.Separator))
	for i, part := range parts {
		if part == "." || part == ".." {
			continue
		}
		if t := strings.TrimRight(part, ". "); t != "" {
			parts[i] = t
		}
	}
	return strings.Join(parts, string(filepath.Separator))
}

// foldRune is the smallest rune in r's Unicode simple-fold cycle, so every
// rune a disk treats as equal maps to one key.
func foldRune(r rune) rune {
	m := r
	for c := unicode.SimpleFold(r); c != r; c = unicode.SimpleFold(c) {
		if c < m {
			m = c
		}
	}
	return m
}

// pathUnder reports whether p is dir or inside it, folding case and width
// where the disk ignores them.
func pathUnder(p, dir string) bool {
	if foldPaths {
		p, dir = foldKey(p), foldKey(dir)
	}
	if p == dir {
		return true
	}
	return strings.HasPrefix(p, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// denyUnder is pathUnder for a deny list: it folds case and width on every
// system. A Linux disk can ignore case too (an ext4 folder with casefold, a
// vfat or exFAT stick), and a wider deny match only refuses more.
func denyUnder(p, dir string) bool {
	p, dir = foldKey(p), foldKey(dir)
	if p == dir {
		return true
	}
	return strings.HasPrefix(p, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// realPath is p with every link in the part of it that exists resolved. The
// part that does not exist yet, such as the folders a mkdir makes, is joined
// on as given. On macOS a path through /System/Volumes/Data is given by its
// short name (trimDataVolume).
func realPath(p string) string {
	rest := ""
	cur := p
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			r = trimDataVolume(r)
			if rest == "" {
				return r
			}
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// realParent is p with the links in its folder resolved and its last name as
// given: the place a write to p lands. A rename onto a link replaces the
// link, so the last name is not followed.
func realParent(p string) string {
	dir, base := filepath.Split(p)
	if base == "" {
		return realPath(p)
	}
	return filepath.Join(realPath(filepath.Clean(dir)), base)
}

// confine turns a path a link gave into the real path its write goes to, so
// the root can name it. A path that is not under a link's roots stays as it
// is, and the root then refuses it.
func (f fileFS) confine(p string) string {
	if f.root == nil {
		return p
	}
	real := realParent(p)
	if foldPaths && pathUnder(real, f.dir) && !strings.HasPrefix(real, f.dir) {
		// The same folder written with other case: the root needs its own
		// spelling for the part above the path.
		real = f.dir + real[len(f.dir):]
	}
	return real
}

// confineDir is confine for a folder that the operation goes into rather
// than acts on, so a link at its last name is followed too: an os.Root
// refuses a link that names an absolute path, even one inside the root.
func (f fileFS) confineDir(p string) string {
	if f.root == nil {
		return p
	}
	real := realPath(p)
	if foldPaths && pathUnder(real, f.dir) && !strings.HasPrefix(real, f.dir) {
		real = f.dir + real[len(f.dir):]
	}
	return real
}

// tildePath writes p under home as ~/..., for a message.
func tildePath(p, home string) string {
	if home != "" && pathUnder(p, home) && len(p) >= len(home) {
		rest := strings.TrimPrefix(p[len(home):], string(filepath.Separator))
		if rest == "" {
			return "~"
		}
		return "~/" + filepath.ToSlash(rest)
	}
	return p
}
