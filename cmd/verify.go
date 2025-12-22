package cmd

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
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

	checkForUpdates := func(prefix string, version string) (int, error) {
		tag := getGitTagFromVersion(prefix, version)

		var buf strings.Builder
		args := []string{"log", "--oneline", fmt.Sprintf("%s..%s", tag, "origin/"+defaultBranch)}
		if isPathBased && prefix != "" {
			args = append(args, "--", prefix)
		}
		err := execCmd(&buf, cmd.OutOrStderr(), "git", args...)
		if err != nil {
			return 0, fmt.Errorf("failed to check git log: %w", err)
		}
		if buf.Len() > 0 {
			return strings.Count(buf.String(), "\n"), nil
		}
		return 0, nil
	}

	type result struct {
		prefix     string
		numChanges int
	}

	var lock sync.Mutex
	var results []*result
	var eg errgroup.Group
	eg.SetLimit(runtime.NumCPU() * 8)
	for _, p := range prefixes {
		eg.Go(func() error {
			var result result
			result.prefix = p
			current, err := getCurrentVersion(p)
			if err != nil {
				return fmt.Errorf("failed to get current version for prefix '%s': %w", p, err)
			}
			result.numChanges, err = checkForUpdates(p, current)
			if err != nil {
				return fmt.Errorf("failed to check for updates for prefix '%s': %w", p, err)
			}
			lock.Lock()
			defer lock.Unlock()
			results = append(results, &result)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	for _, r := range slices.SortedFunc(slices.Values(results), func(a, b *result) int {
		// sort by numChanges ascending, then by prefix alphabetically
		// if numChanges are equal to 0, should appear at the end
		if a.numChanges == b.numChanges {
			return strings.Compare(a.prefix, b.prefix)
		}
		if a.numChanges == 0 {
			return 1
		}
		if b.numChanges == 0 {
			return -1
		}
		return b.numChanges - a.numChanges
	}) {
		if r.numChanges > 0 {
			fmt.Printf("There are %d updates available for prefix '%s'.\n", r.numChanges, r.prefix)
		} else {
			fmt.Printf("There are no updates for prefix '%s'.\n", r.prefix)
		}
	}

	return nil
}
