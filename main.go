package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

var (
	debug      = flag.Bool("v", false, "Enable verbose debugging output")
	term       = flag.Bool("t", true, "Run in a terminal (deprecated, always true)")
	exclude    ArrayString
	watchPath  ArrayString
	cmd        ArrayString
	hasSetPGID bool
)

type ArrayString []string

func (i *ArrayString) String() string {
	return fmt.Sprint(*i)
}

func (i *ArrayString) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	flag.Var(&exclude, "x", "Exclude files and directories matching this regular expression")
	flag.Var(&watchPath, "p", "The path to watch")
	flag.Var(&cmd, "c", "The command when get event")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s: [flags] command [command args…]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	t := reflect.TypeOf(syscall.SysProcAttr{})
	f, ok := t.FieldByName(setpgidName)
	if ok && f.Type.Kind() == reflect.Bool {
		debugPrint("syscall.SysProcAttr.Setpgid exists and is a bool")
		hasSetPGID = true
	} else if ok {
		debugPrint("syscall.SysProcAttr.Setpgid exists but is a %s, not a bool", f.Type.Kind())
	} else {
		debugPrint("syscall.SysProcAttr.Setpgid does not exist")
	}

	if len(cmd) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	if len(cmd) != len(watchPath) {
		log.Fatalln("The number of cmd not equal to watchPath")
		os.Exit(1)
	}

	var wg sync.WaitGroup
	for i, path := range watchPath {
		exc := ""
		if len(exclude) > i {
			exc = exclude[i]
		}

		w, err := fsnotify.NewWatcher()
		if err != nil {
			panic(err)
		}

		c := &Client{
			hasSetPGID: hasSetPGID,
			killChan:   make(chan time.Time, 1),
			path:       path,
			cmd:        cmd[i],
			exclude:    exc,
			fwatch:     w,
		}
		wg.Add(1)
		go c.serve()
	}
	wg.Wait()
}
