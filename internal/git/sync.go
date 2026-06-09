// Package git implements bidirectional sync between a local folder and GitHub.
//
// Convention: repo name = folder basename.
// Viktor pushes his changes to a session branch; we push local state before each task.
//
// Flow per task:
//   EnsureRepo     → local git init + private GitHub repo + remote "viktor"
//   PushLocal      → commit/push current local state so Viktor sees it
//   SessionBranch  → create viktor/{session_id} branch, switch to it
//   ... Viktor works and pushes ...
//   SwitchBack     → return to main branch
//   Pull           → pull Viktor's changes back locally
package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const remote = "viktor" // git remote name — keeps "origin" untouched

var repoNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// RepoInfo holds resolved repo metadata.
type RepoInfo struct {
	Name   string
	Owner  string
	URL    string
	Branch string
}

// SanitizeRepoName converts a folder name to a valid GitHub repo name.
func SanitizeRepoName(raw string) string {
	name := repoNameRe.ReplaceAllString(raw, "-")
	name = regexp.MustCompile(`-{2,}`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-.")
	if name == "" {
		return "repo"
	}
	return name
}

// run executes a command and returns (exitCode, combinedOutput).
func run(dir string, timeout time.Duration, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))

	if ctx.Err() == context.DeadlineExceeded {
		return 1, "timeout: " + text
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), text
		}
		return 1, err.Error() + ": " + text
	}
	return 0, text
}

// DetectOwner resolves the GitHub login via gh CLI (or returns override).
func DetectOwner(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	code, out := run("", 15*time.Second, "gh", "api", "user", "-q", ".login")
	if code != 0 || out == "" {
		return "", fmt.Errorf("could not detect GitHub owner: %s", out)
	}
	return strings.SplitN(out, "\n", 2)[0], nil
}

// CurrentBranch returns the current git branch name.
func CurrentBranch(path string) string {
	code, out := run(path, 10*time.Second, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if code == 0 && out != "" && out != "HEAD" {
		return out
	}
	return "main"
}

// EnsureRepo ensures a local git repo, a private GitHub repo, and the "viktor" remote.
// Idempotent.
func EnsureRepo(path, owner string) (RepoInfo, error) {
	raw := filepath.Base(filepath.Clean(path))
	name := SanitizeRepoName(raw)
	if name == "" {
		return RepoInfo{}, fmt.Errorf("cannot derive repo name from path: %s", path)
	}
	if name != raw {
		fmt.Printf("[git] folder '%s' → repo '%s'\n", raw, name)
	}
	url := fmt.Sprintf("https://github.com/%s/%s.git", owner, name)

	// 1. Local git init
	if _, err := os.Stat(filepath.Join(path, ".git")); os.IsNotExist(err) {
		if code, out := run(path, 15*time.Second, "git", "init", "-b", "main"); code != 0 {
			return RepoInfo{}, fmt.Errorf("git init failed: %s", out)
		}
	}

	// 2. GitHub repo — always private
	code, vis := run("", 20*time.Second, "gh", "repo", "view",
		fmt.Sprintf("%s/%s", owner, name), "--json", "visibility", "-q", ".visibility")
	if code != 0 {
		// Repo doesn't exist — create it
		code2, out2 := run("", 30*time.Second, "gh", "repo", "create",
			fmt.Sprintf("%s/%s", owner, name), "--private")
		if code2 != 0 && !strings.Contains(strings.ToLower(out2), "already exists") {
			return RepoInfo{}, fmt.Errorf("gh repo create failed: %s", out2)
		}
	} else if strings.ToUpper(strings.TrimSpace(vis)) != "PRIVATE" {
		// Make existing public repo private
		run("", 20*time.Second, "gh", "repo", "edit",
			fmt.Sprintf("%s/%s", owner, name),
			"--visibility", "private",
			"--accept-visibility-change-consequences")
	}

	// 3. Remote "viktor"
	code, out := run(path, 10*time.Second, "git", "remote", "get-url", remote)
	if code != 0 {
		run(path, 10*time.Second, "git", "remote", "add", remote, url)
	} else if strings.TrimSpace(out) != url {
		run(path, 10*time.Second, "git", "remote", "set-url", remote, url)
	}

	branch := CurrentBranch(path)
	return RepoInfo{Name: name, Owner: owner, URL: url, Branch: branch}, nil
}

// EnsureLocalExclude adds an entry to .git/info/exclude (local gitignore, not pushed).
func EnsureLocalExclude(path, entry string) {
	excl := filepath.Join(path, ".git", "info", "exclude")
	if _, err := os.Stat(filepath.Dir(excl)); os.IsNotExist(err) {
		return
	}
	content, _ := os.ReadFile(excl)
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
	}
	f, err := os.OpenFile(excl, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		f.WriteString("\n")
	}
	f.WriteString(entry + "\n")
}

// SessionBranch creates viktor/{sessionID} branch, switches to it, pushes to remote.
func SessionBranch(path, sessionID string) (string, error) {
	branch := fmt.Sprintf("viktor/%s", sessionID)

	code, _ := run(path, 10*time.Second, "git", "rev-parse", "--verify", branch)
	if code != 0 {
		run(path, 10*time.Second, "git", "checkout", "-b", branch)
		fmt.Printf("[git] created branch %s\n", branch)
	} else {
		run(path, 10*time.Second, "git", "checkout", branch)
		fmt.Printf("[git] switched to %s\n", branch)
	}

	// Push branch to remote if not there yet
	code, out := run(path, 30*time.Second, "git", "ls-remote", "--heads", remote, branch)
	if code != 0 || strings.TrimSpace(out) == "" {
		run(path, 60*time.Second, "git", "push", "-u", remote, branch)
		fmt.Printf("[git] branch %s pushed to remote\n", branch)
	}

	return branch, nil
}

// SwitchBack returns to the target branch.
func SwitchBack(path, target string) error {
	if target == "" {
		target = "main"
	}
	code, _ := run(path, 10*time.Second, "git", "rev-parse", "--verify", target)
	if code != 0 {
		target = "main"
	}
	code2, out := run(path, 10*time.Second, "git", "checkout", target)
	if code2 != 0 {
		return fmt.Errorf("checkout %s failed: %s", target, out)
	}
	fmt.Printf("[git] returned to %s\n", target)
	return nil
}

// mergeRemote pulls from remote/branch using ff-only → merge → allow-unrelated.
// prefer: "ours" (before push) or "theirs" (after Viktor's work, for pull).
func mergeRemote(path, branch, prefer string) (int, string) {
	code, out := run(path, 60*time.Second, "git", "pull", "--ff-only", remote, branch)
	if code == 0 {
		return code, out
	}
	low := strings.ToLower(out)
	if strings.Contains(low, "couldn't find remote ref") || strings.Contains(low, "no such ref") {
		return 0, out // branch doesn't exist on remote yet
	}

	gitID := []string{"-c", "user.email=viktor2api@local", "-c", "user.name=viktor2api"}
	mergeArgs := append([]string{"git"}, append(gitID,
		"pull", "--no-rebase", "--no-edit", "-X", prefer, remote, branch)...)
	code2, out2 := run(path, 90*time.Second, mergeArgs...)

	if code2 != 0 && strings.Contains(strings.ToLower(out2), "unrelated histories") {
		mergeArgs = append(mergeArgs[:len(mergeArgs)-2],
			append([]string{"--allow-unrelated-histories"}, mergeArgs[len(mergeArgs)-2:]...)...)
		code2, out2 = run(path, 90*time.Second, mergeArgs...)
	}
	if code2 != 0 {
		run(path, 10*time.Second, "git", "merge", "--abort")
	}
	return code2, out2
}

// PushLocal commits any local changes and pushes to remote.
// Returns a human-readable report string, or "" if nothing to push.
func PushLocal(path, branch string) (string, error) {
	gitID := []string{"-c", "user.email=viktor2api@local", "-c", "user.name=viktor2api"}

	run(path, 15*time.Second, "git", "add", "-A")

	hasHead := func() bool {
		c, _ := run(path, 5*time.Second, "git", "rev-parse", "--verify", "HEAD")
		return c == 0
	}

	hasHead0 := hasHead()
	code, _ := run(path, 5*time.Second, "git", "diff", "--cached", "--quiet")
	hasStaged := code != 0

	var report []string

	if hasStaged {
		args := append([]string{"git"}, append(gitID, "commit", "-m", "sync: local changes before task")...)
		run(path, 15*time.Second, args...)
		report = append(report, "local changes committed")
	} else if !hasHead0 {
		args := append([]string{"git"}, append(gitID, "commit", "--allow-empty", "-m", "init")...)
		run(path, 15*time.Second, args...)
		report = append(report, "empty repo initialized")
	}

	// Check if there's anything to push
	code, out := run(path, 60*time.Second, "git", "ls-remote", "--heads", remote, branch)
	remoteSHA := ""
	if code == 0 && strings.TrimSpace(out) != "" {
		parts := strings.Fields(out)
		if len(parts) > 0 {
			remoteSHA = parts[0]
		}
	}

	ahead := 0
	if remoteSHA == "" {
		if hasHead() {
			ahead = 1
		}
	} else {
		code2, out2 := run(path, 10*time.Second, "git", "rev-list", "--count",
			fmt.Sprintf("%s..HEAD", remoteSHA))
		if code2 == 0 {
			fmt.Sscanf(strings.TrimSpace(out2), "%d", &ahead)
		} else {
			ahead = 1
		}
	}

	if ahead == 0 {
		return "", nil // nothing to push
	}

	mergeRemote(path, branch, "ours")

	code3, out3 := run(path, 60*time.Second, "git", "push", "-u", remote, branch)
	if code3 == 0 {
		report = append(report, "local state pushed")
	} else {
		report = append(report, fmt.Sprintf("push warning: %s", out3))
	}

	return strings.Join(report, "; "), nil
}

// Pull pulls Viktor's changes from remote/branch into the local folder.
func Pull(path, branch string) (string, error) {
	code, out := mergeRemote(path, branch, "theirs")
	status := "ok"
	if code != 0 {
		status = fmt.Sprintf("conflict (code %d)", code)
	}
	return fmt.Sprintf("[git pull %s]\n%s", status, out), nil
}

// HasIncoming checks if remote/branch has commits not present locally.
func HasIncoming(path, branch string) bool {
	run(path, 60*time.Second, "git", "fetch", remote, branch)
	code, out := run(path, 10*time.Second, "git", "rev-list", "--count",
		fmt.Sprintf("HEAD..%s/%s", remote, branch))
	if code != 0 {
		return false
	}
	var n int
	fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n > 0
}

// WrapTask wraps the user task with repo context for Viktor.
func WrapTask(task string, repo RepoInfo) string {
	return fmt.Sprintf(
		"[Project context] Work in existing GitHub repo `%s/%s`. "+
			"DO NOT create a new repo. Make the requested changes and push to branch `%s`. "+
			"The current state is already pushed there.\n\nTASK:\n%s",
		repo.Owner, repo.Name, repo.Branch, task,
	)
}
