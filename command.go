package main

import (
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/kr/pty"
)

type Command struct {
	infoLog, stdLog, errLog *ColoredLogger
	lastError               *error
	commandString           string
	cmd                     *exec.Cmd
	workingDir              string
	sync.Mutex
}

func NewCommand(name string, command string, workingDirectory string) *Command {
	return &Command{
		commandString: command,
		workingDir:    workingDirectory,
		infoLog:       NewColoredLogger(name, ColorReset),
		stdLog:        NewColoredLogger(name, ColorWhite),
		errLog:        NewColoredLogger(name, ColorRed),
	}
}

func (c *Command) Start(stopped chan error) { // non-blocking
	// if the command is empty, don't do anything.
	if c.commandString == "" {
		go c.setError(nil, stopped)
		return
	}

	c.Lock()
	if c.cmd != nil {
		c.Stop()
	}

	parts := strings.Split(c.commandString, " ")
	c.cmd = exec.Command(parts[0], parts[1:]...)
	c.cmd.Dir = c.workingDir

	go func() {
		f, tty, err := pty.Open()
		if err != nil {
			go c.setError(nil, stopped)
			return
		}
		defer tty.Close()
		defer f.Close()

		c.cmd.Stdout = tty
		c.cmd.Stderr = tty /*&prefixWriter{
			prefix:     []byte{},
			underlying: tty,
		}*/
		c.cmd.Stdin = tty

		err = c.cmd.Start()

		go io.Copy(c.stdLog, f)

		if err != nil {
			c.setError(err, stopped)
			return
		}

		err = c.cmd.Wait()
		c.setError(err, stopped)
	}()

	c.Unlock()
}

type prefixWriter struct {
	underlying io.Writer
	prefix     []byte
}

func (p *prefixWriter) Write(b []byte) (n int, err error) {
	return p.underlying.Write(b)
	/*
		var buf bytes.Buffer
		buf.Write(p.prefix)
		buf.Write(b)
		bufBytes := buf.Bytes()

		n, err = p.underlying.Write(bufBytes)
		if err != nil {
			return 0, err
		}

		n -= len(p.prefix)
		return n, err*/
}

func (c *Command) setError(err error, stopped chan error) {
	c.Lock()
	defer c.Unlock()
	c.lastError = &err
	c.cmd = nil
	if stopped != nil {
		stopped <- err
	}
}

func (c *Command) Stop() { // non-blocking
	c.Lock()
	defer c.Unlock()
	if c.cmd != nil {
		c.cmd.Process.Kill()
		c.cmd = nil
	}
}

func (c *Command) LastError() error {
	c.Lock()
	defer c.Unlock()
	return *c.lastError
}
