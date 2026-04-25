package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

var (
	defaultProtectedBranches = []string{"main", "master", "develop"}
	gitCommandRunner         = runGitCommand
	errHelpRequested         = errors.New("help requested")
)

type remoteBranch struct {
	remote string
	name   string
}

func (branch remoteBranch) fullName() string {
	return branch.remote + "/" + branch.name
}

type appConfig struct {
	globalProtectedBranches  []string
	projectProtectedBranches []string
}

type deletionPlan struct {
	localBranches  []string
	remoteBranches []remoteBranch
}

type stringList []string

func (list *stringList) String() string {
	return strings.Join(*list, ",")
}

func (list *stringList) Set(value string) error {
	*list = append(*list, value)
	return nil
}

type configFile struct {
	Global struct {
		ProtectedBranches []string `toml:"protected_branches"`
	} `toml:"global"`
	Projects []projectConfig `toml:"project"`
}

type projectConfig struct {
	RepoRoot          string   `toml:"repo_root"`
	ProtectedBranches []string `toml:"protected_branches"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	options, err := parseFlags(args, stdout)
	if err != nil {
		if errors.Is(err, errHelpRequested) {
			return 0
		}
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	repoRoot, err := discoverRepoRoot()
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	if err := fetchPruned(repoRoot); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	protectedBranches, err := resolveProtectedBranches(repoRoot, options.configPath, options.protect)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	plan, err := buildDeletionPlan(repoRoot, options.deleteLocal, options.deleteRemote, options.remotes, protectedBranches)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	renderPlan(stdout, plan, options.dryRun)
	if options.dryRun {
		return 0
	}

	if err := applyDeletions(stdout, repoRoot, plan); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}

	return 0
}

type cliOptions struct {
	dryRun       bool
	deleteLocal  bool
	deleteRemote bool
	remotes      []string
	protect      []string
	configPath   string
}

func parseFlags(args []string, helpOutput io.Writer) (cliOptions, error) {
	flagSet := flag.NewFlagSet("git-branch-prune", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	var (
		localOnly  bool
		remoteOnly bool
		remotes    stringList
		protect    stringList
		configPath string
		dryRun     bool
	)

	flagSet.BoolVar(&localOnly, "local-only", false, "Delete only merged local branches.")
	flagSet.BoolVar(&remoteOnly, "remote-only", false, "Delete only merged remote branches.")
	flagSet.BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without deleting anything.")
	flagSet.Var(&remotes, "remote", "Limit remote deletions to a specific remote. Repeat to allow more than one.")
	flagSet.Var(&protect, "protect", "Add an extra protected branch name or remote ref for this run.")
	flagSet.StringVar(&configPath, "config", defaultConfigPath(), "Configuration file path.")

	flagSet.Usage = func() {
		fmt.Fprintf(flagSet.Output(), "Usage of %s:\n", flagSet.Name())
		flagSet.PrintDefaults()
	}

	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flagSet.SetOutput(helpOutput)
			flagSet.Usage()
			return cliOptions{}, errHelpRequested
		}
		return cliOptions{}, err
	}

	if localOnly && remoteOnly {
		return cliOptions{}, errors.New("--local-only and --remote-only cannot be used together")
	}

	deleteLocal := !remoteOnly
	deleteRemote := !localOnly

	if !deleteRemote && len(remotes) > 0 {
		return cliOptions{}, errors.New("--remote can only be used when remote deletion is enabled")
	}

	return cliOptions{
		dryRun:       dryRun,
		deleteLocal:  deleteLocal,
		deleteRemote: deleteRemote,
		remotes:      append([]string(nil), remotes...),
		protect:      append([]string(nil), protect...),
		configPath:   configPath,
	}, nil
}

func defaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/git-branch-prune/config.toml"
	}

	return filepath.Join(homeDir, ".config", "git-branch-prune", "config.toml")
}

func discoverRepoRoot() (string, error) {
	output, err := gitCommandRunner("", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}

	return canonicalPath(strings.TrimSpace(output))
}

func fetchPruned(repoRoot string) error {
	_, err := gitCommandRunner(repoRoot, "fetch", "--prune")
	return err
}

func resolveProtectedBranches(repoRoot string, configPath string, cliProtected []string) (map[string]struct{}, error) {
	config, err := loadConfig(configPath, repoRoot)
	if err != nil {
		return nil, err
	}

	protected := make(map[string]struct{})
	for _, branch := range defaultProtectedBranches {
		protected[strings.TrimSpace(branch)] = struct{}{}
	}

	for _, branch := range config.globalProtectedBranches {
		branch = strings.TrimSpace(branch)
		if branch != "" {
			protected[branch] = struct{}{}
		}
	}

	for _, branch := range config.projectProtectedBranches {
		branch = strings.TrimSpace(branch)
		if branch != "" {
			protected[branch] = struct{}{}
		}
	}

	for _, branch := range cliProtected {
		branch = strings.TrimSpace(branch)
		if branch != "" {
			protected[branch] = struct{}{}
		}
	}

	currentBranch, err := getCurrentBranch(repoRoot)
	if err != nil {
		return nil, err
	}
	if currentBranch != "" {
		protected[currentBranch] = struct{}{}
	}

	return protected, nil
}

func loadConfig(configPath string, repoRoot string) (appConfig, error) {
	expandedConfigPath, err := expandPath(configPath)
	if err != nil {
		return appConfig{}, err
	}

	data, err := os.ReadFile(expandedConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return appConfig{}, nil
		}
		return appConfig{}, fmt.Errorf("read config: %w", err)
	}

	var parsed configFile
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return appConfig{}, fmt.Errorf("parse config: %w", err)
	}

	canonicalRepoRoot, err := canonicalPath(repoRoot)
	if err != nil {
		return appConfig{}, err
	}

	config := appConfig{
		globalProtectedBranches: append([]string(nil), parsed.Global.ProtectedBranches...),
	}

	for _, project := range parsed.Projects {
		if project.RepoRoot == "" {
			continue
		}
		projectRoot, err := canonicalPath(project.RepoRoot)
		if err != nil {
			return appConfig{}, err
		}
		if projectRoot == canonicalRepoRoot {
			config.projectProtectedBranches = append(config.projectProtectedBranches, project.ProtectedBranches...)
		}
	}

	return config, nil
}

func buildDeletionPlan(repoRoot string, deleteLocal bool, deleteRemote bool, selectedRemotes []string, protectedBranches map[string]struct{}) (deletionPlan, error) {
	plan := deletionPlan{}

	if deleteLocal {
		localBranches, err := listLocalMergedBranches(repoRoot)
		if err != nil {
			return deletionPlan{}, err
		}

		seen := make(map[string]struct{})
		for _, branch := range localBranches {
			if _, protected := protectedBranches[branch]; protected {
				continue
			}
			seen[branch] = struct{}{}
		}

		plan.localBranches = make([]string, 0, len(seen))
		for branch := range seen {
			plan.localBranches = append(plan.localBranches, branch)
		}
		slices.Sort(plan.localBranches)
	}

	if deleteRemote {
		targetRemotes, err := resolveTargetRemotes(repoRoot, selectedRemotes)
		if err != nil {
			return deletionPlan{}, err
		}

		remoteBranches, err := listRemoteMergedBranches(repoRoot)
		if err != nil {
			return deletionPlan{}, err
		}

		seen := make(map[string]remoteBranch)
		for _, branch := range remoteBranches {
			if _, ok := targetRemotes[branch.remote]; !ok {
				continue
			}
			if isProtectedRemoteBranch(branch, protectedBranches) {
				continue
			}
			seen[branch.fullName()] = branch
		}

		plan.remoteBranches = make([]remoteBranch, 0, len(seen))
		for _, branch := range seen {
			plan.remoteBranches = append(plan.remoteBranches, branch)
		}
		slices.SortFunc(plan.remoteBranches, func(left, right remoteBranch) int {
			return strings.Compare(left.fullName(), right.fullName())
		})
	}

	return plan, nil
}

func resolveTargetRemotes(repoRoot string, selectedRemotes []string) (map[string]struct{}, error) {
	availableRemotes, err := listRemotes(repoRoot)
	if err != nil {
		return nil, err
	}

	selected := make(map[string]struct{})
	for _, remote := range selectedRemotes {
		remote = strings.TrimSpace(remote)
		if remote != "" {
			selected[remote] = struct{}{}
		}
	}

	if len(selected) > 0 {
		missing := make([]string, 0)
		for remote := range selected {
			if !slices.Contains(availableRemotes, remote) {
				missing = append(missing, remote)
			}
		}
		slices.Sort(missing)
		if len(missing) > 0 {
			return nil, fmt.Errorf("unknown remote(s): %s", strings.Join(missing, ", "))
		}
		return selected, nil
	}

	if len(availableRemotes) == 0 {
		return map[string]struct{}{}, nil
	}

	if len(availableRemotes) > 1 {
		return nil, fmt.Errorf(
			"multiple remotes detected. Use --remote NAME to choose which remote branches to delete: %s",
			strings.Join(availableRemotes, ", "),
		)
	}

	return map[string]struct{}{availableRemotes[0]: {}}, nil
}

func isProtectedRemoteBranch(branch remoteBranch, protectedBranches map[string]struct{}) bool {
	_, protectedByName := protectedBranches[branch.name]
	_, protectedByFullName := protectedBranches[branch.fullName()]
	return protectedByName || protectedByFullName
}

func listLocalMergedBranches(repoRoot string) ([]string, error) {
	output, err := gitCommandRunner(repoRoot, "branch", "--merged")
	if err != nil {
		return nil, err
	}
	return parseLocalBranchOutput(output), nil
}

func listRemoteMergedBranches(repoRoot string) ([]remoteBranch, error) {
	output, err := gitCommandRunner(repoRoot, "branch", "-r", "--merged")
	if err != nil {
		return nil, err
	}
	return parseRemoteBranchOutput(output), nil
}

func listRemotes(repoRoot string) ([]string, error) {
	output, err := gitCommandRunner(repoRoot, "remote")
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var remotes []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		remotes = append(remotes, line)
	}

	slices.Sort(remotes)
	return remotes, nil
}

func getCurrentBranch(repoRoot string) (string, error) {
	output, err := gitCommandRunner(repoRoot, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func parseLocalBranchOutput(output string) []string {
	branches := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches
}

func parseRemoteBranchOutput(output string) []remoteBranch {
	branches := make([]remoteBranch, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "->") {
			continue
		}

		parts := strings.SplitN(line, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}

		branches = append(branches, remoteBranch{
			remote: parts[0],
			name:   parts[1],
		})
	}
	return branches
}

func renderPlan(stdout io.Writer, plan deletionPlan, dryRun bool) {
	action := "Deleting"
	if dryRun {
		action = "Would delete"
	}

	if len(plan.localBranches) == 0 && len(plan.remoteBranches) == 0 {
		fmt.Fprintln(stdout, "No merged branches to delete.")
		return
	}

	if len(plan.localBranches) > 0 {
		fmt.Fprintf(stdout, "%s local branches:\n", action)
		for _, branch := range plan.localBranches {
			fmt.Fprintf(stdout, "  %s\n", branch)
		}
	}

	if len(plan.remoteBranches) > 0 {
		fmt.Fprintf(stdout, "%s remote branches:\n", action)
		for _, branch := range plan.remoteBranches {
			fmt.Fprintf(stdout, "  %s\n", branch.fullName())
		}
	}
}

func applyDeletions(stdout io.Writer, repoRoot string, plan deletionPlan) error {
	for _, branch := range plan.localBranches {
		if _, err := gitCommandRunner(repoRoot, "branch", "-d", branch); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Deleted local branch: %s\n", branch)
	}

	for _, branch := range plan.remoteBranches {
		if _, err := gitCommandRunner(repoRoot, "push", branch.remote, "--delete", branch.name); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Deleted remote branch: %s\n", branch.fullName())
	}

	return nil
}

func expandPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path cannot be empty")
	}

	if strings.HasPrefix(path, "~/") || path == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			path = homeDir
		} else {
			path = filepath.Join(homeDir, path[2:])
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}

	return absPath, nil
}

func canonicalPath(path string) (string, error) {
	expanded, err := expandPath(path)
	if err != nil {
		return "", err
	}

	canonical, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return expanded, nil
		}
		return "", fmt.Errorf("canonicalize path %q: %w", expanded, err)
	}

	return canonical, nil
}

func runGitCommand(repoRoot string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	if repoRoot != "" {
		command.Dir = repoRoot
	}

	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("`git %s` failed: %s", strings.Join(args, " "), message)
	}

	return string(output), nil
}
