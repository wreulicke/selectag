package main

import (
	"fmt"
	"maps"
	"os"
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

var rootCmd = &cobra.Command{
	Use:   "selectag",
	Short: "Select tag prefix for monorepo module releases",
	Long:  `A CLI tool to help you select the appropriate tag prefix when releasing separated modules in a monorepo.`,
	RunE:  runSelectTag,
}

func init() {
	rootCmd.Flags().StringVarP(&prefix, "prefix", "p", "", "Add additional tag prefix options (can be used multiple times)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runSelectTag(cmd *cobra.Command, args []string) error {
	// detect default branch
	if defaultBranch == "" {
		out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "origin/HEAD").Output()
		if err != nil {
			defaultBranch = "main"
		}
		defaultBranch = strings.TrimSpace(strings.TrimPrefix(string(out), "origin/"))
	}

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
			panic(err)
		}
		v, err := version.NewSemver(currentVersion)
		if err != nil {
			panic(err)
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
	oldVersion, _ := getCurrentVersion(selectedPrefix)
	newTag := getGitTagFromVersion(selectedPrefix, newVersion)
	oldTag := getGitTagFromVersion(selectedPrefix, oldVersion)

	// Output the generated tag
	fmt.Println("\n✓ Tag generated successfully!")
	fmt.Println("─────────────────────────────")
	if selectedPrefix == "" {
		fmt.Printf("Module:  (root)\n")
	} else {
		fmt.Printf("Module:  %s\n", strings.TrimSuffix(selectedPrefix, "/"))
	}
	fmt.Printf("Version: %s\n", newVersion)
	fmt.Printf("Tag:     %s\n", newTag)
	fmt.Println("─────────────────────────────")
	fmt.Printf("\nTo create this tag, run:\n\n")
	fmt.Printf("git tag %s -a -m \"%s\" origin/%s\n", newTag, releaseTitle, defaultBranch)
	fmt.Printf("git push origin %s\n", newTag)
	fmt.Printf("gh release create %s --draft --generate-notes --notes-start-tag %s\n", newTag, oldTag)

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

	cmd := exec.Command("git", "describe", "--tags", fmt.Sprintf("--match=%s**", prefix), "--abbrev=0", fmt.Sprintf("origin/%s", defaultBranch))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current version from git tags: %w", err)
	}
	return strings.TrimPrefix(strings.TrimSpace(string(output)), prefix), nil
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
