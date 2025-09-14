// qobs init [name]
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/zeozeozeo/qobs/internal/msg"
)

func writefile(content string, elem ...string) {
	path := filepath.Join(elem...)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err = os.WriteFile(path, []byte(content), 0o644); err != nil {
			msg.Fatal("create file %s: %v", path, err)
		}
		fmt.Printf("%s file: %s\n", color.HiGreenString("Created"), filepath.ToSlash(path))
	}
}

func mkdir(elem ...string) {
	path := filepath.Join(elem...)
	if err := os.MkdirAll(path, 0o755); err != nil {
		msg.Fatal("mkdir %s: %v", path, err)
	}
}

func getProgramName() string {
	if len(os.Args) == 0 {
		return "qobs"
	}
	basename := filepath.Base(os.Args[0])
	return strings.TrimSuffix(basename, filepath.Ext(basename))
}

// initIn initializes a package in an existing specified directory
func initIn(dir, name string, lib bool) {
	if lib {
		// Qobs.toml
		writefile(`[package]
name = "`+name+`"
description = "This is where I make a project."
authors = ["AzureDiamond"]

[target]
lib = true
sources = ["src/**.cpp", "src/**.cc", "src/**.c"]
headers = ["src/**.hpp", "src/**.h"]

[dependencies]
`, dir, "Qobs.toml")
	} else {
		// Qobs.toml
		writefile(`[package]
name = "`+name+`"
description = "This is where I make a project."
authors = ["AzureDiamond"]

[target]
sources = ["src/**.cpp", "src/**.cc", "src/**.c"]

[dependencies]
`, dir, "Qobs.toml")
	}

	mkdir(dir, "src")

	if lib {
		// src/hello_world.c
		writefile(`#include <stdio.h>
#include "hello_world.h"

void hello_world() {
    puts("Hello, World!");
}
`, dir, "src", "hello_world.c")

		// src/hello_world.h
		writefile(`#ifndef HELLOWORLD_H
#define HELLOWORLD_H

#ifdef __cplusplus
extern "C" {
#endif

void hello_world();

#ifdef __cplusplus
} // extern "C"
#endif

#endif
`, dir, "src", "hello_world.h")
	} else {
		// src/main.c
		writefile(`// You may change this to a .cpp (.cc) file if you'd like
#include <stdio.h>

int main(void) {
    puts("Hello, World!");
    return 0;
}
`, dir, "src", "main.c")
	}

	// .gitignore
	writefile(`build/
`, dir, ".gitignore")

	programName := getProgramName()
	fmt.Printf("You can now do %s to build, or %s to build and run.\n", color.HiCyanString(programName+" "+dir), color.HiCyanString(programName+" run "+dir))
}

var library bool

var initCmd = &cobra.Command{
	Use:   "init [name]",
	Short: "Create a new package in the current directory",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		initIn(".", args[0], library)
	},
}

var newCmd = &cobra.Command{
	Use:   "new [path]",
	Short: "Create a new package in a new directory",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		mkdir(args[0])
		initIn(args[0], filepath.Base(args[0]), library)
	},
}

func init() {
	// qobs init subcommand
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVarP(&library, "lib", "l", false, "Create a library target")

	// qobs new subcommand
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().BoolVarP(&library, "lib", "l", false, "Create a library target")
}
