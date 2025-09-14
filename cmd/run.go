// qobs run [path]
package cmd

import (
	"github.com/spf13/cobra"
	"github.com/zeozeozeo/qobs/internal/builder"
	"github.com/zeozeozeo/qobs/internal/msg"
)

func doRun(cmd *cobra.Command, args []string) {
	target := "."
	if len(args) > 0 {
		target = args[0]
		args = args[1:] // other arguments will be passed to program
	}
	b, err := builder.NewBuilderInDirectory(target)
	if err != nil {
		msg.Fatal("%v", err)
	}
	if err := b.BuildAndRun(args); err != nil {
		msg.Fatal("%v", err)
	}
}

var runCmd = &cobra.Command{
	Use:   "run [target path]",
	Short: "Build and run the package",
	Long:  `Build and run the package. If no target path is given, uses "."`,
	Args:  cobra.ArbitraryArgs,
	Run:   doRun,
}

func init() {
	// qobs run subcommand
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().BoolVarP(&release, "release", "r", false, "Build in release mode")
}
