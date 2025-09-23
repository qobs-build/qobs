package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type EnumValue struct {
	value      string
	allowed    map[string]string // value -> help text
	defaultVal string
}

func NewEnumValue(defaultVal string, allowed map[string]string) EnumValue {
	if _, ok := allowed[defaultVal]; !ok {
		panic(fmt.Sprintf("default value %q not in allowed set", defaultVal))
	}
	return EnumValue{
		value:      defaultVal,
		allowed:    allowed,
		defaultVal: defaultVal,
	}
}

func (e *EnumValue) String() string     { return e.value }
func (e *EnumValue) HelpString() string { return "[" + strings.Join(e.AllowedKeys(), ", ") + "]" }
func (e *EnumValue) Type() string       { return "enum" }
func (e *EnumValue) Value() string      { return e.value }

func (e *EnumValue) Set(v string) error {
	if _, ok := e.allowed[v]; ok {
		e.value = v
		return nil
	}
	return fmt.Errorf("must be one of: %s", strings.Join(e.AllowedKeys(), ", "))
}

func (e *EnumValue) AllowedKeys() []string {
	keys := make([]string, 0, len(e.allowed))
	for k := range e.allowed {
		keys = append(keys, k)
	}
	return keys
}

func (e *EnumValue) CompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		items := make([]string, 0, len(e.allowed))
		for k, help := range e.allowed {
			if help != "" {
				items = append(items, fmt.Sprintf("%s\t%s", k, help))
			} else {
				items = append(items, string(k))
			}
		}
		return items, cobra.ShellCompDirectiveDefault
	}
}
