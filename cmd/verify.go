package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	isPathBased bool
)

func NewVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify that the specified tag has been released",
		Long:  `A CLI tool to verify that a specific version tag exists in the git repository.`,
		RunE:  verify,
	}
	cmd.Flags().StringVarP(&prefix, "prefix", "p", "", "Specify the tag prefix to verify")
	cmd.Flags().BoolVarP(&isPathBased, "path-based", "P", true, "Specify if the verification is path-based")

	return cmd
}

func verify(cmd *cobra.Command, args []string) error {
	// Implementation of the verification logic goes here
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

	checkForUpdates := func(prefix string, version string) (bool, error) {
		tag := getGitTagFromVersion(prefix, version)

		var buf strings.Builder
		args := []string{"log", "--oneline", fmt.Sprintf("%s..%s", tag, "origin/"+defaultBranch)}
		if isPathBased {
			args = append(args, "--", prefix)
		}
		err := execCmd(&buf, cmd.OutOrStderr(), "git", args...)
		if err != nil {
			return false, fmt.Errorf("failed to check git log: %w", err)
		}
		if buf.Len() > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), buf.String())
			return true, nil
		}
		return false, nil
	}
	for _, p := range prefixes {
		current, err := getCurrentVersion(p)
		if err != nil {
			return fmt.Errorf("failed to get current version for prefix '%s': %w", p, err)
		}
		hasUpdates, err := checkForUpdates(p, current)
		if err != nil {
			return fmt.Errorf("failed to check for updates for prefix '%s': %w", p, err)
		}
		if hasUpdates {
			fmt.Printf("There are updates available for prefix '%s'.\n", p)
		} else {
			fmt.Printf("There are no updates for prefix '%s'.\n", p)
		}
	}

	return nil
}
