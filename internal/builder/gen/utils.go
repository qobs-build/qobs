package gen

import "strings"

func write(sb *strings.Builder, s ...string) {
	for _, str := range s {
		sb.WriteString(str)
	}
}
func writeln(sb *strings.Builder, s ...string) {
	for _, str := range s {
		sb.WriteString(str)
	}
	sb.WriteByte('\n')
}
