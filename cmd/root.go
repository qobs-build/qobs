// qobs [path], qobs build [path]
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zeozeozeo/qobs/internal/builder"
	"github.com/zeozeozeo/qobs/internal/msg"
)

var (
	flagProfile   string
	flagGenerator EnumValue = NewEnumValue("qobs", map[string]string{
		"qobs":   "Use Qobs's builder (default)",
		"ninja":  "Generates build.ninja files",
		"vs2022": "Generates Visual Studio 2022 project files",
	})
)

func doBuild(cmd *cobra.Command, args []string) {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}
	b, err := builder.NewBuilderInDirectory(target)
	if err != nil {
		msg.Fatal("%v", err)
	}
	if err := b.Build(flagProfile, flagGenerator.Value()); err != nil {
		msg.Fatal("%v", err)
	}
}

var rootCmd = &cobra.Command{
	Use:   "qobs [target path]",
	Short: "Quite OK Build System",
	Long:  `Quite OK Build System`,
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
	addBuildFlags(rootCmd)

	// qobs build subcommand
	rootCmd.AddCommand(buildCmd)
	addBuildFlags(buildCmd)
}

func addBuildFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&flagProfile, "profile", "p", "debug", "Build with the given profile")
	cmd.Flags().VarP(&flagGenerator, "gen", "g", "Generator to build with, one of "+flagGenerator.HelpString())
	cmd.RegisterFlagCompletionFunc("gen", flagGenerator.CompletionFunc())
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
