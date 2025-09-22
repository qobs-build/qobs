package msg

import (
	"fmt"
	"io"
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

type IndentWriter struct {
	Indent    string
	W         io.Writer
	didIndent bool
}

func (w *IndentWriter) Write(p []byte) (n int, err error) {
	for _, c := range p {
		if !w.didIndent {
			w.W.Write([]byte(w.Indent))
			w.didIndent = true
		}
		w.W.Write([]byte{c}) // FIXME-perf: buffer this
		if c == '\n' || c == '\r' {
			w.didIndent = false
		}
	}
	return len(p), nil
}
