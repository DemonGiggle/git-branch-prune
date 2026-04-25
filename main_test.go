package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func withGitRunner(t *testing.T, runner func(string, ...string) (string, error)) {
	t.Helper()

	previous := gitCommandRunner
	gitCommandRunner = runner
	t.Cleanup(func() {
		gitCommandRunner = previous
	})
}

func TestParseLocalBranchOutput(t *testing.T) {
	output := "  feature/one\n* main\n  feature/two\n"

	branches := parseLocalBranchOutput(output)

	expected := []string{"feature/one", "main", "feature/two"}
	if strings.Join(branches, ",") != strings.Join(expected, ",") {
		t.Fatalf("unexpected branches: got %v want %v", branches, expected)
	}
}

func TestParseRemoteBranchOutputSkipsSymbolicRefs(t *testing.T) {
	output := "  origin/HEAD -> origin/main\n  origin/feature/one\n  upstream/bugfix\n"

	branches := parseRemoteBranchOutput(output)

	if len(branches) != 2 {
		t.Fatalf("unexpected branch count: got %d", len(branches))
	}

	if branches[0] != (remoteBranch{remote: "origin", name: "feature/one"}) {
		t.Fatalf("unexpected first branch: %#v", branches[0])
	}

	if branches[1] != (remoteBranch{remote: "upstream", name: "bugfix"}) {
		t.Fatalf("unexpected second branch: %#v", branches[1])
	}
}

func TestLoadConfigMergesGlobalAndProjectEntries(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configPath := filepath.Join(tempDir, "config.toml")
	configBody := strings.Join([]string{
		"[global]",
		`protected_branches = ["release"]`,
		"",
		"[[project]]",
		`repo_root = "` + repoRoot + `"`,
		`protected_branches = ["demo"]`,
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := loadConfig(configPath, repoRoot)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if strings.Join(config.globalProtectedBranches, ",") != "release" {
		t.Fatalf("unexpected global branches: %v", config.globalProtectedBranches)
	}

	if strings.Join(config.projectProtectedBranches, ",") != "demo" {
		t.Fatalf("unexpected project branches: %v", config.projectProtectedBranches)
	}
}

func TestResolveProtectedBranchesIncludesDefaultsConfigCLIAndCurrentBranch(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configPath := filepath.Join(tempDir, "config.toml")
	configBody := strings.Join([]string{
		"[global]",
		`protected_branches = ["release"]`,
		"",
		"[[project]]",
		`repo_root = "` + repoRoot + `"`,
		`protected_branches = ["demo"]`,
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	withGitRunner(t, func(repoRoot string, args ...string) (string, error) {
		if strings.Join(args, " ") == "branch --show-current" {
			return "feature/current\n", nil
		}
		t.Fatalf("unexpected git command: %q", strings.Join(args, " "))
		return "", nil
	})

	protected, err := resolveProtectedBranches(repoRoot, "feature/current", configPath, []string{"cli-only"})
	if err != nil {
		t.Fatalf("resolve protected branches: %v", err)
	}

	for _, branch := range []string{"main", "master", "develop", "release", "demo", "cli-only", "feature/current"} {
		if _, ok := protected[branch]; !ok {
			t.Fatalf("missing protected branch %q in %v", branch, protected)
		}
	}
}

func TestBuildDeletionPlanFiltersProtectedBranches(t *testing.T) {
	withGitRunner(t, func(repoRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "branch --merged":
			return "  feature/two\n  develop\n  feature/one\n", nil
		case "remote":
			return "origin\n", nil
		case "branch -r --merged":
			return "  origin/feature/one\n  origin/main\n", nil
		default:
			t.Fatalf("unexpected git command: %q", strings.Join(args, " "))
			return "", nil
		}
	})

	plan, err := buildDeletionPlan("/repo", true, true, nil, map[string]struct{}{
		"main":    {},
		"develop": {},
	})
	if err != nil {
		t.Fatalf("build deletion plan: %v", err)
	}

	if strings.Join(plan.localBranches, ",") != "feature/one,feature/two" {
		t.Fatalf("unexpected local plan: %v", plan.localBranches)
	}

	if len(plan.remoteBranches) != 1 || plan.remoteBranches[0] != (remoteBranch{remote: "origin", name: "feature/one"}) {
		t.Fatalf("unexpected remote plan: %v", plan.remoteBranches)
	}
}

func TestBuildDeletionPlanRequiresExplicitRemoteWhenMultipleExist(t *testing.T) {
	withGitRunner(t, func(repoRoot string, args ...string) (string, error) {
		if strings.Join(args, " ") == "remote" {
			return "origin\nupstream\n", nil
		}
		t.Fatalf("unexpected git command: %q", strings.Join(args, " "))
		return "", nil
	})

	_, err := buildDeletionPlan("/repo", false, true, nil, map[string]struct{}{})
	if err == nil || !strings.Contains(err.Error(), "multiple remotes detected") {
		t.Fatalf("expected multiple remotes error, got %v", err)
	}
}

func TestRunDryRunPrintsPlannedDeletions(t *testing.T) {
	withGitRunner(t, func(repoRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --show-toplevel":
			return "/repo\n", nil
		case "fetch --prune":
			return "", nil
		case "branch --show-current":
			return "main\n", nil
		case "branch --merged":
			return "  feature/one\n", nil
		case "remote":
			return "origin\n", nil
		case "branch -r --merged":
			return "  origin/feature/two\n", nil
		default:
			t.Fatalf("unexpected git command: %q", strings.Join(args, " "))
			return "", nil
		}
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--dry-run"}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d stderr=%q", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "Merge base: main\n") {
		t.Fatalf("expected merge base in stdout, got %q", output)
	}
	if !strings.Contains(output, "Would delete local branches:") || !strings.Contains(output, "feature/one") {
		t.Fatalf("unexpected stdout: %q", output)
	}
	if !strings.Contains(output, "Would delete remote branches:") || !strings.Contains(output, "origin/feature/two") {
		t.Fatalf("unexpected stdout: %q", output)
	}
}

func TestRunListProtectedPrintsEffectiveProtectedBranchesWithoutFetching(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configPath := filepath.Join(tempDir, "config.toml")
	configBody := strings.Join([]string{
		"[global]",
		`protected_branches = ["release"]`,
		"",
		"[[project]]",
		`repo_root = "` + repoRoot + `"`,
		`protected_branches = ["demo"]`,
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	withGitRunner(t, func(currentRepoRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --show-toplevel":
			return repoRoot + "\n", nil
		case "branch --show-current":
			return "feature/current\n", nil
		case "fetch --prune":
			t.Fatal("did not expect fetch when listing protected branches")
		default:
			t.Fatalf("unexpected git command: %q", strings.Join(args, " "))
		}
		return "", nil
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--list-protected", "--config", configPath, "--protect", "origin/release"}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d stderr=%q", exitCode, stderr.String())
	}

	expected := strings.Join([]string{
		"Merge base: feature/current",
		"Protected branches:",
		"  demo",
		"  develop",
		"  feature/current",
		"  main",
		"  master",
		"  origin/release",
		"  release",
		"",
	}, "\n")
	if stdout.String() != expected {
		t.Fatalf("unexpected stdout: got %q want %q", stdout.String(), expected)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunPromptsBeforeDeletionAndAbortsByDefault(t *testing.T) {
	withGitRunner(t, func(repoRoot string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --show-toplevel":
			return "/repo\n", nil
		case "branch --show-current":
			return "main\n", nil
		case "fetch --prune":
			return "", nil
		case "branch --merged":
			return "  feature/one\n", nil
		case "remote":
			return "origin\n", nil
		case "branch -r --merged":
			return "  origin/feature/two\n", nil
		case "branch -d feature/one", "push origin --delete feature/two":
			t.Fatalf("did not expect deletion command: %q", strings.Join(args, " "))
		default:
			t.Fatalf("unexpected git command: %q", strings.Join(args, " "))
		}
		return "", nil
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{}, strings.NewReader("\n"), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d stderr=%q", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "Merge base: main\n") {
		t.Fatalf("expected merge base in stdout, got %q", output)
	}
	if !strings.Contains(output, "Proceed with deleting these branches? [y/N]: ") {
		t.Fatalf("expected confirmation prompt, got %q", output)
	}
	if !strings.Contains(output, "Aborted.") {
		t.Fatalf("expected abort message, got %q", output)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunYesSkipsPromptAndDeletesBranches(t *testing.T) {
	var commands []string
	withGitRunner(t, func(repoRoot string, args ...string) (string, error) {
		command := strings.Join(args, " ")
		commands = append(commands, command)

		switch command {
		case "rev-parse --show-toplevel":
			return "/repo\n", nil
		case "branch --show-current":
			return "main\n", nil
		case "fetch --prune":
			return "", nil
		case "branch --merged":
			return "  feature/one\n", nil
		case "remote":
			return "origin\n", nil
		case "branch -r --merged":
			return "  origin/feature/two\n", nil
		case "branch -d feature/one":
			return "", nil
		case "push origin --delete feature/two":
			return "", nil
		default:
			t.Fatalf("unexpected git command: %q", command)
		}
		return "", nil
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--yes"}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d stderr=%q", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "Merge base: main\n") {
		t.Fatalf("expected merge base in stdout, got %q", output)
	}
	if strings.Contains(output, "Proceed with deleting these branches? [y/N]: ") {
		t.Fatalf("did not expect confirmation prompt, got %q", output)
	}
	if !strings.Contains(output, "Deleted local branch: feature/one") {
		t.Fatalf("expected local deletion output, got %q", output)
	}
	if !strings.Contains(output, "Deleted remote branch: origin/feature/two") {
		t.Fatalf("expected remote deletion output, got %q", output)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !slices.Contains(commands, "branch -d feature/one") || !slices.Contains(commands, "push origin --delete feature/two") {
		t.Fatalf("expected deletion commands, got %v", commands)
	}
}

func TestRunYesAcceptsInteractiveConfirmation(t *testing.T) {
	var commands []string
	withGitRunner(t, func(repoRoot string, args ...string) (string, error) {
		command := strings.Join(args, " ")
		commands = append(commands, command)

		switch command {
		case "rev-parse --show-toplevel":
			return "/repo\n", nil
		case "branch --show-current":
			return "main\n", nil
		case "fetch --prune":
			return "", nil
		case "branch --merged":
			return "  feature/one\n", nil
		case "remote":
			return "origin\n", nil
		case "branch -r --merged":
			return "", nil
		case "branch -d feature/one":
			return "", nil
		default:
			t.Fatalf("unexpected git command: %q", command)
		}
		return "", nil
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{}, strings.NewReader("y\n"), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d stderr=%q", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "Merge base: main\n") {
		t.Fatalf("expected merge base in stdout, got %q", output)
	}
	if !strings.Contains(output, "Proceed with deleting these branches? [y/N]: ") {
		t.Fatalf("expected confirmation prompt, got %q", output)
	}
	if !strings.Contains(output, "Deleted local branch: feature/one") {
		t.Fatalf("expected deletion output, got %q", output)
	}
	if slices.Contains(commands, "push origin --delete feature/two") {
		t.Fatalf("did not expect remote deletion command, got %v", commands)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}
