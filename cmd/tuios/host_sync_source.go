package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/tuios/internal/childproc"
	"github.com/Gaurav-Gosain/tuios/internal/release"
)

// Where `tuios hosts sync` gets its binaries: the release that matches this
// build, a build of a checkout, or a file the person names.

// syncBuildTimeout bounds one cross-compile.
const syncBuildTimeout = 10 * time.Minute

func newSyncBinary(data []byte, ver string) *syncBinary {
	sum := sha256.Sum256(data)
	return &syncBinary{data: data, sha: hex.EncodeToString(sum[:]), version: ver}
}

// loadBinary reads a --binary file and the platform it is for.
func (s *syncSource) loadBinary(p string) error {
	s.kind = sourceBinary
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	s.binPath = abs
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	goos, goarch, err := binaryPlatform(data)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	// The module path is in every Go binary built from the tuios main
	// package, stripped or not.
	if !bytes.Contains(data, []byte("github.com/"+release.Repo+"/cmd/tuios")) {
		return fmt.Errorf("%s is not a tuios binary", p)
	}
	s.platform = goos + "/" + goarch
	// The version is read by running it, which only works for this
	// machine's own system.
	if goos == runtime.GOOS && goarch == runtime.GOARCH {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, abs, "--version").Output()
		if err != nil {
			return fmt.Errorf("%s did not run: %w", p, err)
		}
		line, _, _ := strings.Cut(string(out), "\n")
		s.version, _ = parseVersionLine(line)
	}
	s.built[s.platform] = newSyncBinary(data, s.version)
	return nil
}

// initDev reads the version a build of the checkout in dir reports:
// dev+COMMIT, and for a tree with changes dev+COMMIT-dirty.HASH, where HASH
// is of the changes. Two different sets of changes give two versions, so a
// host with the last build is not taken to be up to date.
func (s *syncSource) initDev(dir string) error {
	s.kind = sourceDev
	s.srcDir = dir
	git := func(args ...string) (string, error) {
		cmd := childproc.Git(context.Background(), dir, args...)
		out, err := cmd.Output()
		return string(out), err
	}
	rev, err := git("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("could not read the commit of %s: %w", dir, err)
	}
	rev = strings.TrimSpace(rev)
	s.commit = rev
	s.version = "dev+" + rev[:min(12, len(rev))]
	if when, err := git("log", "-1", "--format=%cI"); err == nil {
		s.date = strings.TrimSpace(when)
	}
	status, err := git("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("could not read the state of %s: %w", dir, err)
	}
	if strings.TrimSpace(status) == "" {
		return nil
	}
	h := sha256.New()
	diff, err := git("diff", "HEAD", "--binary")
	if err != nil {
		return fmt.Errorf("could not read the changes in %s: %w", dir, err)
	}
	_, _ = io.WriteString(h, diff)
	untracked, _ := git("ls-files", "--others", "--exclude-standard", "-z")
	for name := range strings.SplitSeq(untracked, "\x00") {
		if name == "" {
			continue
		}
		_, _ = io.WriteString(h, "\x00"+name+"\x00")
		if f, err := os.Open(filepath.Join(dir, name)); err == nil {
			_, _ = io.Copy(h, io.LimitReader(f, 64<<20))
			_ = f.Close()
		}
	}
	s.commit += "-dirty"
	s.version += "-dirty." + hex.EncodeToString(h.Sum(nil))[:8]
	return nil
}

// buildDev cross-compiles the checkout for one platform with the release
// flags.
func (s *syncSource) buildDev(platform string) (*syncBinary, error) {
	goos, goarch, _ := strings.Cut(platform, "/")
	out := filepath.Join(s.tmpDir, "tuios-"+goos+"-"+goarch)
	fmt.Fprintf(s.errOut, "Building tuios %s for %s...\n", s.version, platform)
	ldflags := "-s -w -X main.version=" + s.version + " -X main.commit=" + s.commit + " -X main.builtBy=hosts-sync"
	if s.date != "" {
		ldflags += " -X main.date=" + s.date
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncBuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", ldflags, "-o", out, "./cmd/tuios")
	cmd.Dir = s.srcDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the build for %s failed: %v: %s", platform, err, lastLine(stderr.String()))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	return newSyncBinary(data, s.version), nil
}

// lookupRelease reads the release of this version and its checksums.
func (s *syncSource) lookupRelease() error {
	if s.rel != nil {
		return nil
	}
	tag := "v" + strings.TrimPrefix(s.version, "v")
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	gh := release.NewGitHub()
	rel, err := gh.Tag(ctx, tag)
	if err != nil {
		if errors.Is(err, release.ErrNoRelease) {
			return &diagnosticError{
				What:  fmt.Sprintf("GitHub has no release %s.", tag),
				Cause: "sync installs the release that matches this build.",
				Fix:   "run the command in a tuios checkout with --dev, or pass --binary PATH",
			}
		}
		return explainLookupError(err, false)
	}
	sums, err := fetchChecksums(ctx, gh, rel)
	if err != nil {
		return err
	}
	s.rel, s.sums = &rel, sums
	return nil
}

// fetchRelease downloads and checks the archive for one platform.
func (s *syncSource) fetchRelease(platform string) (*syncBinary, error) {
	goos, goarch, _ := strings.Cut(platform, "/")
	name, err := release.AssetName("tuios", s.rel.Tag, goos, goarch)
	if err != nil {
		return nil, fmt.Errorf("no release archive fits %s: %w", platform, err)
	}
	asset, ok := s.rel.AssetNamed(name)
	if !ok {
		return nil, fmt.Errorf("%s publishes no %s", s.rel.Tag, name)
	}
	fmt.Fprintf(s.errOut, "Downloading %s...\n", name)
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	archive, err := readAsset(ctx, release.NewGitHub(), asset)
	if err != nil {
		return nil, err
	}
	if err := s.sums.Verify(name, archive); err != nil {
		return nil, explainChecksumError(err, *s.rel)
	}
	data, err := release.BinaryFromArchive(bytes.NewReader(archive), "tuios")
	if err != nil {
		return nil, fmt.Errorf("%s did not hold tuios: %w", name, err)
	}
	return newSyncBinary(data, s.version), nil
}

// binaryPlatform reads the system and architecture an executable is for from
// its header. It knows the ELF and Mach-O builds that sync can install, and
// is kept to a few header fields: debug/elf and debug/macho would add their
// whole parsers to the binary for two numbers.
func binaryPlatform(data []byte) (goos, goarch string, err error) {
	switch {
	case len(data) >= 20 && bytes.HasPrefix(data, []byte("\x7fELF")):
		if data[5] != 1 {
			return "", "", errors.New("the binary is a big-endian ELF file, which sync does not install")
		}
		goos = "linux"
		if data[7] == 9 { // ELFOSABI_FREEBSD
			goos = "freebsd"
		}
		switch binary.LittleEndian.Uint16(data[18:20]) {
		case 0x3e:
			goarch = "amd64"
		case 0xb7:
			goarch = "arm64"
		case 0x03:
			goarch = "386"
		case 0xf3:
			goarch = "riscv64"
		}
	case len(data) >= 8 && binary.LittleEndian.Uint32(data[0:4]) == 0xfeedfacf:
		goos = "darwin"
		switch binary.LittleEndian.Uint32(data[4:8]) {
		case 0x01000007:
			goarch = "amd64"
		case 0x0100000c:
			goarch = "arm64"
		}
	default:
		return "", "", errors.New("the file is not a Linux, macOS or FreeBSD executable")
	}
	if goarch == "" {
		return "", "", errors.New("the binary is for an architecture sync does not install")
	}
	return goos, goarch, nil
}

// networkTimeout bounds one release lookup or download.
const networkTimeout = 3 * time.Minute

// releasePage is where to send someone who has to do this by hand.
func releasePage(rel release.Release) string {
	if rel.URL != "" {
		return rel.URL
	}
	return "https://github.com/" + release.Repo + "/releases"
}

// fetchChecksums reads the release's digest list.
//
// A release with no checksums.txt is refused rather than installed unverified.
// The check is the only thing standing between a redirected download and a
// binary this command puts on PATH, and a check that is skipped whenever it is
// inconvenient is not a check.
func fetchChecksums(ctx context.Context, src release.Source, rel release.Release) (release.Checksums, error) {
	asset, ok := rel.AssetNamed(release.ChecksumFile)
	if !ok {
		return nil, &diagnosticError{
			What:  fmt.Sprintf("%s publishes no %s.", rel.Tag, release.ChecksumFile),
			Cause: "Without it there is nothing to check a download against, and this will not install an unverified binary.",
			Fix:   "download and verify the archive by hand from " + releasePage(rel),
		}
	}
	data, err := readAsset(ctx, src, asset)
	if err != nil {
		return nil, err
	}
	sums, err := release.ParseChecksums(bytes.NewReader(data))
	if err != nil {
		return nil, &diagnosticError{
			What:  "The release's checksum list could not be read.",
			Cause: err.Error(),
			Fix:   "download and verify the archive by hand from " + releasePage(rel),
		}
	}
	return sums, nil
}

// maxAssetSize bounds a single download, so a redirect to something enormous
// cannot be read into memory.
const maxAssetSize = 512 << 20

func readAsset(ctx context.Context, src release.Source, asset release.Asset) ([]byte, error) {
	body, err := src.Fetch(ctx, asset.URL)
	if err != nil {
		return nil, explainLookupError(err, false)
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, maxAssetSize+1))
	if err != nil {
		return nil, &diagnosticError{
			What:  "The download did not finish.",
			Cause: err.Error(),
			Fix:   "check the network and try again.",
		}
	}
	if int64(len(data)) > maxAssetSize {
		return nil, &diagnosticError{
			What:  asset.Name + " is larger than this command will read.",
			Cause: "A release archive is tens of megabytes. Something answered with far more than that.",
			Fix:   "download it by hand and check what you are being served.",
		}
	}
	return data, nil
}

// explainChecksumError turns a failed verification into a message that says
// what it means. A mismatch is not a hiccup to retry past.
func explainChecksumError(err error, rel release.Release) error {
	if mismatch, ok := errors.AsType[*release.ChecksumMismatch](err); ok {
		return &diagnosticError{
			What:  "The downloaded archive is not the one the release published.",
			Cause: "Its checksum does not match. The download was corrupted, or something between here and GitHub changed it.",
			Extra: []string{
				"Expected: " + mismatch.Want,
				"Got:      " + mismatch.Got,
				"Nothing was installed and the old binary is untouched.",
			},
			Fix: "try again. If it fails the same way, download the archive by hand from " +
				releasePage(rel) + " and check it yourself.",
		}
	}
	return &diagnosticError{
		What:  "The download could not be verified.",
		Cause: err.Error(),
		Extra: []string{"Nothing was installed and the old binary is untouched."},
		Fix:   "download and verify the archive by hand from " + releasePage(rel),
	}
}

// explainLookupError turns a network or API failure into an instruction.
func explainLookupError(err error, prerelease bool) error {
	if limit, ok := errors.AsType[*release.RateLimitError](err); ok {
		e := &diagnosticError{
			What:  "GitHub is rate limiting this address.",
			Cause: "Sixty release lookups an hour are allowed without a token, shared by everyone on this address.",
			Fix:   "wait and run it again, or set GITHUB_TOKEN to a token with no scopes to raise the limit.",
		}
		if !limit.Reset.IsZero() {
			e.Extra = []string{"The limit resets at " + limit.Reset.Local().Format(time.Kitchen) + "."}
		}
		return e
	}
	if errors.Is(err, release.ErrNoRelease) {
		what := "This repository has published no release."
		if prerelease {
			what = "This repository has published no release or prerelease."
		}
		return &diagnosticError{
			What:  what,
			Cause: "There is nothing to update to.",
			Fix:   "see https://github.com/" + release.Repo + "/releases",
		}
	}
	if httpErr, ok := errors.AsType[*release.HTTPError](err); ok {
		return &diagnosticError{
			What:  "GitHub refused the release lookup.",
			Cause: httpErr.Error(),
			Fix:   "try again shortly. If it keeps failing, check https://www.githubstatus.com.",
		}
	}
	// Anything that reaches here is a transport failure: no route, DNS that
	// answers nothing, a proxy that hung up. They read the same to a user, so
	// they get one message.
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return &diagnosticError{
			What:  "GitHub could not be reached.",
			Cause: err.Error(),
			Fix:   "check the network, or set HTTPS_PROXY if this machine goes out through a proxy.",
		}
	}
	return &diagnosticError{
		What:  "The release lookup failed.",
		Cause: err.Error(),
		Fix:   "check the network and try again.",
	}
}
