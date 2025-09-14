package msg

import (
	"fmt"
	"os"

	"github.com/fatih/color"
)

func Error(format string, a ...any) {
	fmt.Print(color.HiRedString("error"))
	fmt.Print(": ")
	fmt.Printf(format, a...)
	fmt.Print("\n")
}

func Warn(format string, a ...any) {
	fmt.Print(color.YellowString("warn"))
	fmt.Print(": ")
	fmt.Printf(format, a...)
	fmt.Print("\n")
}

func Fatal(format string, a ...any) {
	fmt.Print(color.RedString("fatal"))
	fmt.Print(": ")
	fmt.Printf(format, a...)
	fmt.Print("\n")
	os.Exit(1)
}

func Info(format string, a ...any) {
	fmt.Print(color.HiGreenString("info"))
	fmt.Print(": ")
	fmt.Printf(format, a...)
	fmt.Print("\n")
}
