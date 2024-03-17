package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"time"
)

type AutoGoConfig struct {
	WatchRoot   string       `json:"watchroot"`
	Compilers   []*Compiler  `json:"compilers"`
	Runners     []*Runner    `json:"runners"`
	HttpProxies []*HttpProxy `json:"httpproxies"`
}

type Runner struct {
	Command    string            `json:"command"`
	Name       string            `json:"name"`
	WorkingDir string            `json:"workingdir"`
	Replace    map[string]string `json:"replace"`
}

type Compiler struct {
	Name       string            `json:"name"`
	Pattern    string            `json:"pattern"`
	Exclude    string            `json:"exclude"`
	Command    string            `json:"command"`
	WorkingDir string            `json:"workingdir"`
	RunOnStart bool              `json:"runonstart"`
	Replace    map[string]string `json:"replace"`
}

func main() {
	// parse arguments from command line
	configPath := flag.String("c", "", "config file path")
	libraryConfig := flag.Bool("l", false, "use library default config")
	testConfig := flag.Bool("t", false, "use test config")
	wait := flag.Int("wait", 0, "help message for flagname")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to file")

	flag.Parse()

	// increase ulimit
	err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{
		Cur: 1024 * 50,
		Max: 1024 * 50,
	})
	if err != nil {
		log.Fatal(err)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *wait > 0 {
		for i := 0; i != *wait; i++ {
			fmt.Println("debug running #", i)
			time.Sleep(time.Second)
		}
		fmt.Println("debug done. dying.")
		return
	}

	// default configuration
	config := &AutoGoConfig{WatchRoot: "."}
	if *libraryConfig {
		config.Compilers = []*Compiler{
			{Name: "build", Pattern: "*.go", Command: "go build -o .tmp_autogo/autogo_build"},
		}
	} else if *testConfig {
		config.Compilers = []*Compiler{
			{Name: "test", Pattern: "*.go", Command: "go test"},
		}
	} else {
		config.Compilers = []*Compiler{
			{Name: "build", Pattern: "*.go", Command: "go build -o .tmp_autogo/autogo_build"},
			{Name: "template", Pattern: "*.tmpl", Command: ""},
		}
		config.Runners = []*Runner{
			{Name: "run", Command: ".tmp_autogo/autogo_build"},
		}
		config.HttpProxies = []*HttpProxy{
			{Listen: ":1984", Target: "http://127.0.0.1:3000"},
		}
	}

	// if directory contains autogo.config, use it!
	if _, err := os.Stat("autogo.config"); !os.IsNotExist(err) && *configPath == "" {
		*configPath = "autogo.config"
	}

	// read config from configPath, if any.
	if *configPath != "" {
		if _, err := os.Stat(*configPath); err != nil {
			log.Panic("Can't find config file", *configPath)
		}

		configBytes, err := ioutil.ReadFile(*configPath)
		if err != nil {
			log.Panic("Can't read config file", *configPath, "error:", err)
		}

		// reset config to empty config and read json into it.
		config = &AutoGoConfig{}
		if err := json.Unmarshal(configBytes, &config); err != nil {
			log.Panic("Can't parse config file", *configPath, "error:", err)
		}
	}

	// create compiler group
	compilers := NewCompilerGroup()

	// start all proxies
	for _, proxy := range config.HttpProxies {
		err := proxy.Start(compilers)
		if err != nil {
			log.Panic("Could not start HTTP proxy server. Error:", err)
		}
	}

	// expand working directories
	for _, compiler := range config.Compilers {
		compiler.WorkingDir, err = filepath.Abs(compiler.WorkingDir)
		if err != nil {
			log.Fatal(err)
		}
	}
	for _, runner := range config.Runners {
		runner.WorkingDir, err = filepath.Abs(runner.WorkingDir)
		if err != nil {
			log.Fatal(err)
		}
	}

	// listen for changes; run compilers when changed.
	var wg sync.WaitGroup
	root, err := filepath.Abs(config.WatchRoot)
	if err != nil {
		log.Panic("Could not watch directory: " + config.WatchRoot)
	}
	for _, compiler := range config.Compilers {
		wg.Add(1)
		go func(compiler *Compiler) {
			// create a watcher to listen for changes
			watcher := NewWatcher()
			watcher.Listen(getFolders(root, compiler), compiler.Pattern, compiler.Exclude)

			if compiler.RunOnStart {
				commandString := compiler.Command
				compile := NewCommand(compiler.Name, commandString, compiler.WorkingDir, compiler.Replace)
				key := compiler.Name + commandString
				compilers.StartCompile(key, compile)
				compile.infoLog.Println("Building")
			}

			wg.Done()

			//getFiles := recursiveGet
			for file := range watcher.Changed {
				// start a compile cycle
				commandString := strings.Replace(compiler.Command, "$filerelative", file[len(compiler.WorkingDir)+1:], 100)
				commandString = strings.Replace(commandString, "$file", file, 100)
				commandString = strings.Replace(commandString, "$wd", compiler.WorkingDir, 100)

				compile := NewCommand(compiler.Name, commandString, compiler.WorkingDir, compiler.Replace)
				key := compiler.Name + commandString
				compilers.StartCompile(key, compile)
				compile.infoLog.Println("Building")

				// refresh listening targets.
				watcher.Listen(getFolders(root, compiler), compiler.Pattern, compiler.Exclude)
			}
		}(compiler)
	}

	wg.Wait()

	// start all runners.
	// runners run when: 1) no compilers running 2) no compile errors
	for _, runner := range config.Runners {
		go func(runner *Runner) {
			cmd := NewCommand(runner.Name, runner.Command, runner.WorkingDir, runner.Replace)

			for {
				stopped := make(chan error)
				go func() {
					err := <-stopped
					if err == nil {
						cmd.infoLog.Println("<end>")
					} else {
						cmd.infoLog.Println(fmt.Sprintf("<end: %s>", err))
					}
				}()
				compilers.WaitForState(Idle)
				cmd.Start(stopped)
				compilers.WaitForState(Compiling)
				cmd.Stop()
			}
		}(runner)
	}

	// keep running.
	done := make(chan bool)
	<-done
}

func getFolders(root string, compiler *Compiler) map[string]bool {
	result := make(map[string]bool)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			if len(path) > 1 && strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}

			result[path] = true
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	if compiler.Pattern == "*.go" {
		gopath := os.Getenv("GOPATH")
		processed := make(map[string]bool)
		findImportDir(root, gopath, result, processed)
	}

	return result
}

func findImportDir(dir string, gopath string, result map[string]bool, processed map[string]bool) {
	if _, ok := processed[dir]; !ok {
		processed[dir] = true
		fd, err := os.Open(dir)
		if err != nil {
			return
		}
		defer fd.Close()

		list, err := fd.Readdir(-1)
		if err != nil {
			log.Fatal(err)
		}

		fset := token.NewFileSet()
		for _, d := range list {
			if strings.HasSuffix(d.Name(), ".go") {
				bytes, err := ioutil.ReadFile(filepath.Join(dir, d.Name()))
				if err != nil {
					log.Fatal(err)
				} else {
					src, err := parser.ParseFile(fset, "", bytes, parser.ImportsOnly)
					if err == nil {
						result[dir] = true

						for _, i := range src.Imports {
							rel := i.Path.Value[1 : len(i.Path.Value)-1]
							findImportDir(filepath.Join(dir, rel), gopath, result, processed)
							findImportDir(filepath.Join(gopath, "/src/", rel), gopath, result, processed)
						}
					}
				}
			}
		}
	}
}
