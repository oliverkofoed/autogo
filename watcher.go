package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

var updateLock sync.RWMutex
var monitoredLock sync.RWMutex
var monitored = make(map[string]map[*Watcher]bool) // directory path => true/false
var fsWatcher *fsnotify.Watcher

type Watcher struct {
	directories     map[string]bool
	patterns        []string
	excludePatterns []string
	timers          map[string]*time.Timer
	timersLock      sync.RWMutex
	triggerLock     sync.RWMutex
	Changed         chan string
}

func NewWatcher() *Watcher {
	return &Watcher{
		Changed: make(chan string),
		timers:  make(map[string]*time.Timer), // directory -> timer
	}
}

func (w *Watcher) Listen(directories map[string]bool, pattern string, excludePattern string) {
	// one person at a time in here!
	updateLock.Lock()
	defer updateLock.Unlock()

	// stop watching old directories
	if len(w.directories) > 0 {
		for dir := range w.directories {
			if _, found := directories[dir]; !found {
				stopWatching(dir, w)
			}
		}
	}

	// start watching new
	for dir := range directories {
		if _, found := w.directories[dir]; !found {
			startWatching(dir, w)
		}
	}

	// save
	w.directories = directories
	w.patterns = make([]string, 0)
	for _, str := range strings.Split(pattern, ",") {
		if x := strings.TrimSpace(str); x != "" {
			w.patterns = append(w.patterns, x)
		}
	}
	w.excludePatterns = make([]string, 0)
	for _, str := range strings.Split(excludePattern, ",") {
		if x := strings.TrimSpace(str); x != "" {
			w.excludePatterns = append(w.excludePatterns, x)
		}
	}
}

func match(pattern, path string) bool {
	// no slashes indicate matching EVERYWHERE.
	if !strings.Contains(pattern, "/") { // "*.something" or "somefile.ext" or "fdssf*fdsfs*.txt"
		match, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil {
			log.Fatal("Could not match paths.", err)
			return false
		}
		return match
	}

	fullPath, err := filepath.Abs(".")
	if err == nil {
		relPath, err := filepath.Rel(fullPath, path)
		if err == nil {
			//a, b := filepath.Match(pattern, relPath)
			//fmt.Println("Matching ", relPath, " with ", pattern, "-------", a, b)
			if exclude, err := filepath.Match(pattern, relPath); exclude && err == nil {
				return true
			}
		} else {
			log.Fatal("Could not get relpath", err)
			return false
		}
	} else {
		log.Fatal("Could not get fullpath", err)
		return false
	}

	return false
}

func (w *Watcher) trigger(path string) {
	for _, pattern := range w.patterns {
		if match(pattern, path) {
			for _, excludePattern := range w.excludePatterns {
				if match(excludePattern, path) {
					return
				}
			}

			w.timersLock.Lock()

			if w.timers[path] != nil {
				w.timers[path].Stop()
			}

			stat, err := os.Stat(path)
			if err != nil || time.Now().Sub(stat.ModTime()) < time.Second {
				w.timers[path] = time.AfterFunc(time.Millisecond*250, func() {
					// send the changed event
					w.triggerLock.Lock()
					w.Changed <- path
					w.triggerLock.Unlock()
				})
			}

			w.timersLock.Unlock()
			return
		}
	}
}

// ----------

func startWatching(directory string, watcher *Watcher) {
	monitoredLock.Lock()
	defer monitoredLock.Unlock()

	// create watcher if it does not exist already.
	if fsWatcher == nil {
		var err error
		fsWatcher, err = fsnotify.NewWatcher()
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			for {
				select {
				case ev := <-fsWatcher.Events:
					if ev.Op != fsnotify.Chmod {
						dir := filepath.Dir(ev.Name)

						monitoredLock.RLock()
						watcherMap, ok := monitored[dir]
						monitoredLock.RUnlock()
						if ok {
							for watcher := range watcherMap {
								watcher.trigger(ev.Name)
							}
						}
					}
				case err := <-fsWatcher.Errors:
					log.Fatal(err)
				}
			}
		}()
	}

	watcherMap, ok := monitored[directory]
	if !ok {
		fsWatcher.Add(directory)
		watcherMap = make(map[*Watcher]bool)
	}

	watcherMap[watcher] = true

	monitored[directory] = watcherMap
}

func stopWatching(directory string, watcher *Watcher) {
	monitoredLock.Lock()
	defer monitoredLock.Unlock()

	if watcherMap, ok := monitored[directory]; ok {
		delete(watcherMap, watcher)
	}
}
