package cmd

import (
	"fmt"
	"io"
	"log"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"
	version "github.com/hashicorp/go-version"
	"github.com/spf13/cobra"
)

var (
	prefix        string
	defaultBranch string
)

func NewRootCommand() *cobra.Command {
	// detect default branch
	if defaultBranch == "" {
		out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "origin/HEAD").Output()
		if err != nil {
			defaultBranch = "main"
		}
		defaultBranch = strings.TrimSpace(strings.TrimPrefix(string(out), "origin/"))
	}

	cmd := &cobra.Command{
		Use:   "selectag",
		Short: "Select tag prefix for monorepo module releases",
		Long:  `A CLI tool to help you select the appropriate tag prefix when releasing separated modules in a monorepo.`,
		RunE:  runSelectTag,
	}
	cmd.Flags().StringVarP(&prefix, "prefix", "p", "", "Add additional tag prefix options (can be used multiple times)")

	cmd.AddCommand(NewVerifyCommand())
	return cmd
}

func runSelectTag(cmd *cobra.Command, args []string) error {

	// Collect tag prefixes from git tags
	var prefixes []string
	var err error
	if prefix == "root" {
		prefixes = []string{""}
	} else if prefix != "" {
		prefixes = []string{strings.TrimSpace(prefix)}
	} else {
		prefixes, err = collectTagPrefixesFromGit()
		if err != nil {
			return fmt.Errorf("failed to collect git tag prefixes: %w", err)
		}
	}

	// Convert map back to slice
	if len(prefixes) == 0 {
		return fmt.Errorf("no tag prefixes found. Either create git tags (e.g., git tag v1.0.0) or use --prefix flag")
	}

	// Prepare options for the select menu
	var options []huh.Option[string]

	for _, prefix := range prefixes {
		if prefix == "" {
			options = append(options, huh.NewOption("(root) - No prefix", ""))
		} else {
			label := prefix
			if strings.HasSuffix(prefix, "/") {
				label = strings.TrimSuffix(prefix, "/")
			}
			options = append(options, huh.NewOption(label, prefix))
		}
	}

	var selectedPrefix string
	var newVersion string
	var releaseTitle string

	generateNewVersionOptions := func() []huh.Option[string] {
		currentVersion, err := getCurrentVersion(selectedPrefix)
		if err != nil {
			panic(fmt.Sprintf("failed to get current version: %v", err))
		}
		v, err := version.NewSemver(currentVersion)
		if err != nil {
			panic(fmt.Sprintf("failed to parse current version: %v", err))
		}
		segments := v.Segments()

		major := fmt.Sprintf("%d.0.0", segments[0]+1)
		minor := fmt.Sprintf("%d.%d.0", segments[0], segments[1]+1)
		patch := fmt.Sprintf("%d.%d.%d", segments[0], segments[1], segments[2]+1)
		suggestions := []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("patch - %s", patch), patch),
			huh.NewOption(fmt.Sprintf("minor - %s", minor), minor),
			huh.NewOption(fmt.Sprintf("major - %s", major), major),
		}
		if len(v.Prerelease()) > 0 {
			// also suggest removing prerelease
			cleanVersion := fmt.Sprintf("%d.%d.%d", segments[0], segments[1], segments[2])
			suggestions = append([]huh.Option[string]{huh.NewOption(fmt.Sprintf("remove prerelease - %s", cleanVersion), cleanVersion)}, suggestions...)
		}
		return suggestions
	}

	// Create the interactive form with module selection and version input
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a tag prefix for your release").
				Description("Choose the module you want to release").
				Options(options...).
				Value(&selectedPrefix),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a new version").
				Description("Choose new version").
				Value(&newVersion).
				OptionsFunc(generateNewVersionOptions, &selectedPrefix).
				Validate(validateVersion),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("release title").
				Description("Enter the release title").
				Placeholder("e.g., release some feature").
				Value(&releaseTitle),
		),
	)

	// Run the form
	if err := form.Run(); err != nil {
		return fmt.Errorf("form error: %w", err)
	}

	// Generate the full tag
	oldVersion, err := getCurrentVersion(selectedPrefix)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}
	newTag := getGitTagFromVersion(selectedPrefix, newVersion)
	oldTag := getGitTagFromVersion(selectedPrefix, oldVersion)

	err = execCmd(cmd.OutOrStdout(), cmd.OutOrStderr(), "git", "tag", newTag, "-a", "-m", releaseTitle, "origin/"+defaultBranch)
	if err != nil {
		return fmt.Errorf("failed to create git tag: %w", err)
	}
	log.Println("Created git tag:", newTag)

	if !continued("Do you want to push the tag and create a GitHub release now?") {
		return nil
	}

	err = execCmd(cmd.OutOrStdout(), cmd.OutOrStderr(), "git", "push", "origin", newTag)
	if err != nil {
		return fmt.Errorf("failed to push git tag: %w", err)
	}
	log.Println("Pushed git tag to origin:", newTag)

	if !continued("Do you want to create a GitHub release now?") {
		return nil
	}

	err = execCmd(cmd.OutOrStdout(), cmd.OutOrStderr(), "gh", "release", "create", newTag, "--draft", "--generate-notes", "--notes-start-tag", oldTag, "--fail-on-no-commits")
	if err != nil {
		return fmt.Errorf("failed to create GitHub release: %w", err)
	}
	log.Println("Created GitHub release for tag:", newTag)

	return nil
}

// validateVersion checks if the version string is valid using go-version
func validateVersion(s string) error {
	if s == "" {
		return fmt.Errorf("version cannot be empty")
	}

	// Add 'v' prefix if not present for validation
	versionStr := s
	if !strings.HasPrefix(versionStr, "v") {
		versionStr = "v" + versionStr
	}

	// Parse and validate using go-version
	_, err := version.NewSemver(versionStr)
	if err != nil {
		return fmt.Errorf("invalid version format: %w", err)
	}

	return nil
}

func getCurrentVersion(prefix string) (string, error) {
	if prefix == "" {
		prefix = "v"
	} else {
		prefix = prefix + "/v"
	}

	cmd := exec.Command("git", "tag", "--list", fmt.Sprintf("%s**", prefix), "--sort=-v:refname")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list git tags: %w", err)
	}

	tags := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found with prefix %s", prefix)
	}

	slices.SortFunc(tags, func(i, j string) int {
		vi, err1 := version.NewSemver(strings.TrimPrefix(i, prefix))
		vj, err2 := version.NewSemver(strings.TrimPrefix(j, prefix))
		if err1 != nil || err2 != nil {
			return 0
		}
		if vi.GreaterThan(vj) {
			return -1
		}
		return 1
	})
	return strings.TrimPrefix(strings.TrimSpace(tags[0]), prefix), nil
}

func getGitTagFromVersion(prefix, version string) string {
	if prefix == "" {
		return "v" + version
	}
	return prefix + "/v" + version
}

// collectTagPrefixesFromGit collects unique tag prefixes from existing git tags
func collectTagPrefixesFromGit() ([]string, error) {
	// Run git tag -l to list all tags
	cmd := exec.Command("git", "tag", "-l")
	output, err := cmd.Output()
	if err != nil {
		// If git command fails, it might not be a git repo or no tags exist
		// Return empty slice instead of error to allow fallback to go.mod detection
		return []string{}, nil
	}

	tags := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(tags) == 1 && tags[0] == "" {
		// No tags found
		return []string{}, nil
	}

	// Regex pattern to match version suffix: /v followed by digits
	// This captures everything before the version as the prefix
	versionPattern := regexp.MustCompile(`^((.*)/v\d+|v\d+)`)

	prefixMap := make(map[string]struct{})

	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}

		// Try to extract prefix using regex
		matches := versionPattern.FindStringSubmatch(tag)
		if len(matches) > 2 {
			prefix := matches[2]
			prefixMap[prefix] = struct{}{}
		} else if len(matches) > 1 {
			// Tag is like v1.0.0 with no prefix
			prefixMap[""] = struct{}{}
		}
	}

	return slices.Collect(maps.Keys(prefixMap)), nil
}

func execCmd(stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	log.Println("Executing command:", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func continued(title string) bool {
	var c bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Value(&c),
		),
	).Run()
	if err != nil {
		panic(fmt.Sprintf("failed to get confirmation: %v", err))
	}
	return c
}
