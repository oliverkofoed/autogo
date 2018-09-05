package main

import (
	"fmt"
	"sync"
	"time"
)

type CompilerGroupState int

const (
	Idle           = 0
	IdleWithErrors = 1
	Compiling      = 2
)

type CompilerGroup struct {
	condition *sync.Cond
	sync.Mutex
	state         CompilerGroupState
	compileFiles  map[string]*Command
	compileErrors map[string]*error
}

func NewCompilerGroup() *CompilerGroup {
	compilers := &CompilerGroup{
		state:         Idle,
		compileFiles:  make(map[string]*Command),
		compileErrors: make(map[string]*error),
	}
	compilers.condition = sync.NewCond(compilers)
	return compilers
}

func (c *CompilerGroup) StartCompile(key string, cmd *Command) {
	c.Lock()

	// remove current (if any)
	if currentCommand := c.compileFiles[key]; currentCommand != nil {
		currentCommand.Stop()
	}

	// add new compiler and update state
	delete(c.compileErrors, key)
	c.compileFiles[key] = cmd
	c.state = Compiling
	c.condition.Broadcast()
	c.Unlock()

	// give a chance for whatever programs we've killed to output to the log.
	time.Sleep(time.Millisecond * 25)

	// start the compiler
	startTime := time.Now()
	stoppedChan := make(chan error)
	cmd.Start(stoppedChan)
	go func() {
		err := <-stoppedChan
		if err == nil {
			cmd.infoLog.Println(fmt.Sprintf("<end: %v>", time.Since(startTime)))
		} else {
			cmd.errLog.Println(fmt.Sprintf("<end: %v>", err))
		}

		c.Lock()
		delete(c.compileFiles, key)
		if err == nil {
			delete(c.compileErrors, key)
		} else {
			c.compileErrors[key] = &err
		}

		// update the state
		if len(c.compileFiles) > 0 {
			c.state = Compiling
		} else if len(c.compileErrors) > 0 {
			c.state = IdleWithErrors
		} else {
			c.state = Idle
		}
		//cmd.infoLog.Println(fmt.Sprintf("new state: %v, key: %v, errors: %v", c.state, key, c.compileErrors))
		c.condition.Broadcast()
		c.Unlock()
	}()
}

func (c *CompilerGroup) WaitForState(waitState CompilerGroupState) {
	c.Lock()
	for c.state != waitState {
		c.condition.Wait()
	}
	c.Unlock()
}
