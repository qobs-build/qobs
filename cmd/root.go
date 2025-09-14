// qobs [path], qobs build [path]
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zeozeozeo/qobs/internal/builder"
	"github.com/zeozeozeo/qobs/internal/msg"
)

var release bool

func doBuild(cmd *cobra.Command, args []string) {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}
	b, err := builder.NewBuilderInDirectory(target)
	if err != nil {
		msg.Fatal("%v", err)
	}
	if _, err := b.Build(); err != nil {
		msg.Fatal("%v", err)
	}
}

var rootCmd = &cobra.Command{
	Use:   "qobs [target path]",
	Short: "Quite OK Build System",
	Long:  `Joyful C/C++ build system and package manager`,
	Args:  cobra.MinimumNArgs(1),
	Run:   doBuild,
}

var buildCmd = &cobra.Command{
	Use:   "build [target path]",
	Short: "Build the package",
	Long:  `Build the package. If no target path is given, uses "."`,
	Args:  cobra.MaximumNArgs(1),
	Run:   doBuild,
}

func init() {
	rootCmd.Flags().BoolVarP(&release, "release", "r", false, "Build in release mode")

	// qobs build subcommand
	rootCmd.AddCommand(buildCmd)
	buildCmd.Flags().BoolVarP(&release, "release", "r", false, "Build in release mode")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
