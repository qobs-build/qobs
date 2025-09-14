package msg

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type ProgressBar struct {
	Total      int64
	Current    int64
	Indent     int
	Start      time.Time
	W          io.Writer
	lastPrint  time.Time
	throbIndex int
}

var throbbers = []rune{'|', '/', '-', '\\'}

func NewProgressBar(total int64, indent int, w io.Writer) *ProgressBar {
	return &ProgressBar{
		Total:     total,
		Indent:    indent,
		Start:     time.Now(),
		W:         w,
		lastPrint: time.Now(),
	}
}

func (pb *ProgressBar) Write(p []byte) (int, error) {
	n := len(p)
	pb.Current += int64(n)

	if time.Since(pb.lastPrint) > 40*time.Millisecond {
		pb.print(false)
		pb.lastPrint = time.Now()
	}
	return n, nil
}

func (pb *ProgressBar) print(finish bool) {
	width := 40
	percent := float64(pb.Current) / float64(max(pb.Total, 1))
	if finish {
		percent = 1
	}

	filled := min(int(percent*float64(width)), width)
	bar := strings.Repeat("█", filled) + strings.Repeat("-", width-filled)

	throb := throbbers[pb.throbIndex%len(throbbers)]
	pb.throbIndex++
	if finish {
		throb = ' '
	}

	if pb.Total > 0 {
		fmt.Fprintf(pb.W, "\r%s%6.f%% [%s] %c",
			strings.Repeat(" ", pb.Indent),
			percent*100,
			bar,
			throb,
		)
	} else {
		fmt.Fprintf(pb.W, "\r%s%d KB %c",
			strings.Repeat(" ", pb.Indent),
			pb.Current/1024,
			throb,
		)
	}
}

func (pb *ProgressBar) Finish() {
	pb.print(true)
	fmt.Fprintln(pb.W)
}
