// qobs index
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qobs-build/qobs/internal/index"
	"github.com/qobs-build/qobs/internal/msg"
	"github.com/spf13/cobra"
)

// ensureLocalIndex loads qobs_index.json from cwd or fails
func ensureLocalIndex() (*index.Index, string) {
	cwd, err := os.Getwd()
	if err != nil {
		msg.Fatal("could not get current directory: %v", err)
	}
	indexPath := filepath.Join(cwd, index.IndexFilename)
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		msg.Fatal("no %s found in current directory (must run inside the qobs index; create it if you need a new index)", index.IndexFilename)
	}

	idx, err := index.ParseIndexInPath(cwd)
	if err != nil {
		msg.Fatal("failed to parse index: %v", err)
	}
	return idx, cwd
}

func doIndexAdd(url, dir string) {
	idx, cwd := ensureLocalIndex()

	if idx.HasDep(url) {
		msg.Warn("overwriting existing dependency for %s", url)
	}
	idx.SetDep(url, dir)

	if err := idx.Save(cwd); err != nil {
		msg.Fatal("failed to save index: %v", err)
	}
	msg.Info("added dependency %s -> %s", url, dir)
}

func doIndexRemove(url string) {
	idx, cwd := ensureLocalIndex()

	if !idx.RemoveDep(url) {
		msg.Warn("dependency %s not found", url)
	} else {
		msg.Info("removed dependency %s", url)
	}

	if err := idx.Save(cwd); err != nil {
		msg.Fatal("failed to save index: %v", err)
	}
}

func doIndexUpdate() {
	_, err := index.UpdateGlobalIndex()
	if err != nil {
		msg.Fatal("failed to update global index: %v", err)
	}
	msg.Info("updated global index successfully")
}

func doIndexSearch(term string) {
	idx, err := index.GetIndexAnyhow()
	if err != nil {
		msg.Fatal("failed to load global index: %v", err)
	}

	term = strings.ToLower(term)
	i := 0
	for url, path := range idx.Deps {
		if strings.Contains(strings.ToLower(url), term) ||
			strings.Contains(strings.ToLower(path), term) {
			fmt.Printf("%d. %s -> %s\n", i+1, url, path)
			i++
		}
	}

	if i == 0 {
		msg.Warn("no matches found for %q", term)
	} else {
		msg.Info("found %d matches for %q", i, term)
	}
}

var indexAddCmd = &cobra.Command{
	Use:   "add <url> <dir>",
	Short: "Add a dependency to the local index",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		doIndexAdd(args[0], args[1])
	},
}

var indexRemoveCmd = &cobra.Command{
	Use:   "remove <url>",
	Short: "Remove a dependency from the local index",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		doIndexRemove(args[0])
	},
}

var indexUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the global cached index",
	Run: func(cmd *cobra.Command, args []string) {
		doIndexUpdate()
	},
}

var indexSearchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "Search the global index for dependencies",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		doIndexSearch(args[0])
	},
}

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Manage the dependency index",
}

func init() {
	// qobs index subcommand
	indexCmd.AddCommand(indexUpdateCmd)
	indexCmd.AddCommand(indexAddCmd)
	indexCmd.AddCommand(indexRemoveCmd)
	indexCmd.AddCommand(indexSearchCmd)
	rootCmd.AddCommand(indexCmd)
}
