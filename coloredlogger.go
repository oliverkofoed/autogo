package main

import (
	"bytes"
	"fmt"
)

const (
	ColorReset   = "0"
	ColorRed     = "31"
	ColorBlack   = "30"
	ColorGreen   = "32"
	ColorYellow  = "33"
	ColorBlue    = "34"
	ColorMagenta = "35"
	ColorCyan    = "36"
	ColorWhite   = "37"
)

type ColoredLogger struct {
	name           string
	color          string
	unfinishedLine *string
	buffer         bytes.Buffer
}

func NewColoredLogger(name string, color string) *ColoredLogger {
	return &ColoredLogger{name: name, color: color}
}

func (c *ColoredLogger) Write(p []byte) (n int, err error) {
	n, err = c.buffer.Write(p)
	if err != nil {
		panic(err.Error())
		return
	}

	for {
		str, err := c.buffer.ReadString('\n')
		if err == nil {
			if c.unfinishedLine != nil {
				str = *c.unfinishedLine + str
			}
			fmt.Printf("\033[%sm\033[%sm%20s: %s\033[%sm", ColorReset, c.color, c.name, str, ColorReset)
			c.unfinishedLine = nil
		} else {
			if c.unfinishedLine != nil {
				str = *c.unfinishedLine + str
			}
			c.unfinishedLine = &str
			break
		}
	}

	return
}

func (c *ColoredLogger) Println(values ...interface{}) {
	fmt.Fprintln(c, values...)
}
