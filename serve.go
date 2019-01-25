package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const rebuildDelay = 200 * time.Millisecond

// The name of the syscall.SysProcAttr.Setpgid field.
const setpgidName = "Setpgid"

type ui interface {
	redisplay(func(io.Writer))
	// An empty struct is sent when the command should be rerun.
	rerun() <-chan struct{}
}

type writerUI struct{ io.Writer }

func (w writerUI) redisplay(f func(io.Writer)) { f(w) }

func (w writerUI) rerun() <-chan struct{} { return nil }

type Client struct {
	hasSetPGID bool
	killChan   chan time.Time
	path       string
	cmd        string
	exclude    string
	excludeRe  *regexp.Regexp
	fwatch     *fsnotify.Watcher
}

func (c *Client) serve() {
	timer := time.NewTimer(0)
	changes := c.startWatching()
	lastRun := time.Time{}
	lastChange := time.Now()

	if c.exclude != "" {
		var err error
		c.excludeRe, err = regexp.Compile(c.exclude)
		if err != nil {
			log.Fatalln("Bad regexp: ", c.exclude)
		}
	}
	ui := ui(writerUI{os.Stdout})

	for {
		select {
		case lastChange = <-changes:
			timer.Reset(rebuildDelay)

		case <-ui.rerun():
			lastRun = c.run(ui)

		case <-timer.C:
			if lastRun.Before(lastChange) {
				lastRun = c.run(ui)
			}
		}
	}
}

func (c *Client) run(ui ui) time.Time {
	ui.redisplay(func(out io.Writer) {
		str := strings.Split(c.cmd, " ")
		cmd := exec.Command(str[0], str[1:]...)
		cmd.Stdout = out
		cmd.Stderr = out
		fmt.Println(out)
		if c.hasSetPGID {
			var attr syscall.SysProcAttr
			reflect.ValueOf(&attr).Elem().FieldByName(setpgidName).SetBool(true)
			cmd.SysProcAttr = &attr
		}
		io.WriteString(out, c.cmd)
		start := time.Now()
		if err := cmd.Start(); err != nil {
			io.WriteString(out, "fatal: "+err.Error()+"\n")
			return
		}
		if s := c.wait(start, cmd); s != 0 {
			io.WriteString(out, "exit status "+strconv.Itoa(s)+"\n")
		}
		io.WriteString(out, time.Now().String()+"\n")
	})

	return time.Now()
}

func (c *Client) wait(start time.Time, cmd *exec.Cmd) int {
	var n int
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case t := <-c.killChan:
			if t.Before(start) {
				continue
			}
			p := cmd.Process.Pid
			if c.hasSetPGID {
				p = -p
			}
			if n == 0 {
				debugPrint("Sending SIGTERM")
				syscall.Kill(p, syscall.SIGTERM)
			} else {
				debugPrint("Sending SIGKILL")
				syscall.Kill(p, syscall.SIGKILL)
			}
			n++
		case <-ticker.C:
			var status syscall.WaitStatus
			p := cmd.Process.Pid
			switch q, err := syscall.Wait4(p, &status, syscall.WNOHANG, nil); {
			case err != nil:
				panic(err)
			case q > 0:
				cmd.Wait() // Clean up any goroutines created by cmd.Start.
				return status.ExitStatus()
			}
		}
	}
}

func (c *Client) kill() {
	select {
	case c.killChan <- time.Now():
		debugPrint("Killing")
	}
}

func (c *Client) startWatching() <-chan time.Time {
	switch isdir, err := isDir(c.path); {
	case err != nil:
		log.Fatalf("Failed to watch %s: %s", c.path, err)
	case isdir:
		c.watchDir(c.path)
	default:
		c.watch(c.path)
	}

	changes := make(chan time.Time)

	go c.sendChanges(changes)

	return changes
}

func (c *Client) sendChanges(changes chan<- time.Time) {
	for {
		select {
		case err := <-c.fwatch.Errors:
			log.Fatalf("Watcher error: %s\n", err)
		case ev := <-c.fwatch.Events:
			if c.excludeRe != nil && c.excludeRe.MatchString(ev.Name) {
				debugPrint("ignoring event for excluded %s", ev.Name)
				continue
			}
			time, err := c.modTime(ev.Name)
			if err != nil {
				log.Printf("Failed to get even time: %s", err)
				continue
			}

			debugPrint("%s at %s", ev, time)

			if ev.Op&fsnotify.Create != 0 {
				switch isdir, err := isDir(ev.Name); {
				case err != nil:
					log.Printf("Couldn't check if %s is a directory: %s", ev.Name, err)
					continue

				case isdir:
					c.watchDir(ev.Name)
				}
			}

			changes <- time
		}
	}
}

func (c *Client) modTime(p string) (time.Time, error) {
	switch s, err := os.Stat(p); {
	case os.IsNotExist(err):
		q := path.Dir(p)
		if q == p {
			err := errors.New("Failed to find directory for " + p)
			return time.Time{}, err
		}
		return c.modTime(q)
	case err != nil:
		return time.Time{}, err

	default:
		return s.ModTime(), nil
	}
}

func (c *Client) watchDir(p string) {
	ents, err := ioutil.ReadDir(p)
	switch {
	case os.IsNotExist(err):
		return
	case err != nil:
		log.Printf("Failed to watch %s: %s", p, err)
	}
	for _, e := range ents {
		sub := path.Join(p, e.Name())
		if c.excludeRe != nil && c.excludeRe.MatchString(sub) {
			debugPrint("excluding %s", sub)
			continue
		}
		switch isdir, err := isDir(sub); {
		case err != nil:
			log.Printf("Failed to watch %s: %s", sub, err)
		case isdir:
			c.watchDir(sub)
		}
	}
	c.watch(p)
}

func (c *Client) watch(p string) {
	debugPrint("Watching %s", p)
	switch err := c.fwatch.Add(p); {
	case os.IsNotExist(err):
		debugPrint("%s no longer exists", p)
	case err != nil:
		log.Printf("Failed to watch %s: %s", p, err)
	}
}

func isDir(p string) (bool, error) {
	switch s, err := os.Stat(p); {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, err
	default:
		return s.IsDir(), nil
	}
}

func debugPrint(f string, vals ...interface{}) {
	if *debug {
		log.Printf("DEBUG: "+f, vals...)
	}
}
