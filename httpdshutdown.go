// Package httpdshutdown implements some convenience functions for cleanly shutting down
// an http daemon. See the README.md file for a full example.

// Package httpdshutdown allows for graceful shutdown of http daemons. This package
// exports a `Watcher` that exports methods for signal handling, connection state
// observation, and shutdown hook registration and execution.
package httpdshutdown

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ShutdownHook is the type callers will implement in their own daemon shutdown handlers.
type ShutdownHook func() error

// Watcher manages the execution of shutdownHooks.
type Watcher struct {
	connsWG       *sync.WaitGroup // Allows us to wait for conns to complete.
	shutdownHooks []ShutdownHook  // Run these when daemon is done or timed out.
	timeoutMS     int             // Grace period for daemon shutdown.
}

// NewWatcher construct a Watcher with a timeout and an optional set of shutdown hooks
// to be called at the time of shutdown.
//
// The first argument is a timeout in milliseconds that will trigger shutdown hooks
// even if the daemon still has open connections. Further arguments are a variadic
// list of type `ShutDownHook`.
//
// Example instantiation:
//
//     watcher, watcher_err := httpdshutdown.NewWatcher(2000, sampleShutdownHook1, sampleShutdownHook2)
//
func NewWatcher(timeoutMS int, hooks ...ShutdownHook) (*Watcher, error) {
	if timeoutMS < 0 {
		return nil, errors.New("timeout must be a positive number")
	}
	w := new(Watcher)
	w.timeoutMS = timeoutMS
	w.connsWG = new(sync.WaitGroup)
	w.shutdownHooks = make([]ShutdownHook, len(hooks))
	copy(w.shutdownHooks, hooks)
	return w, nil
}

// RecordConnState counts open and closed connections.
// This function can be assigned to a `http.Server`'s `ConnState` field.
//
// Example use:
//
//    srv := &http.Server{
//            Addr: ":8080",
//            ReadTimeout:  3 * time.Second,
//            WriteTimeout: 3 * time.Second,
//            ConnState: func(conn net.Conn, newState http.ConnState) {
//                    log.Printf("(1) NEW CONN STATE:%v\n", newState)
//                    watcher.RecordConnState(newState)
//            }
//    }
//
func (w *Watcher) RecordConnState(newState http.ConnState) {
	if w == nil {
		// we panic here instead of returning nil as the calling context does not
		// do any error checking
		panic("RecordConnState: receiver is nil")
	}
	switch newState {
	case http.StateNew:
		w.connsWG.Add(1)
	case http.StateClosed, http.StateHijacked:
		w.connsWG.Done()
	}
}

// RunHooks executes registered hooks, each of which blocks. Typically this is called
// automatically by `OnStop`.
func (w *Watcher) RunHooks() error {
	if w == nil {
		return errors.New("RunHooks: receiver is nil")
	}
	errStrs := make([]string, 0)
	for _, f := range w.shutdownHooks {
		err := f()
		if err != nil {
			errStrs = append(errStrs, "shutdown hook err: "+err.Error())
		}
	}
	if len(errStrs) != 0 {
		return errors.New(strings.Join(errStrs, "\n"))
	}
	return nil
}

// OnStop will be called by a daemon's signal handler when it is time to shutdown. If there
// are any shutdown handlers, they will be called. The timeout set on the watcher will
// be honored. Typically this is called via `SigHandle` as your signal handler.
func (w *Watcher) OnStop() error {
	if w == nil {
		return errors.New("OnStop: receiver is nil")
	}
	waitChan := make(chan bool, 1)
	go func() {
		w.connsWG.Wait()
		waitChan <- true
	}()
	select {
	case <-waitChan:
		_ = w.RunHooks()
		return nil
	case <-time.After(time.Duration(w.timeoutMS) * time.Millisecond):
		_ = w.RunHooks()
		return errors.New("OnStop: shutdown timed out")
	}
}

// SigHandle is an example of a typical signal handler that will attempt a graceful shutdown
// for a set of known signals. The first argument is your signal channel, and the second
// argument is the channel that can be polled for exit status codes.
//
// This should be called prior to starting your http daemon. Place it in its own goroutine
// so signals can be recorded after the daemon has taken over control of the main thread.
//
// Example use:
//
//         go func() {
//                 sigs := make(chan os.Signal, 1)
//                 exitcode := make(chan int, 1)
//                 signal.Notify(sigs)
//                 go watcher.SigHandle(sigs, exitcode)
//                 code := <-exitcode
//                 log.Printf("exit with code:%d", code)
//                 os.Exit(code)
// 	}()
func (w *Watcher) SigHandle(sigs <-chan os.Signal, exitcode chan<- int) {
	if w == nil {
		// panic since this will typically be launched as a goroutine.
		panic("SigHandler: Watcher is nil")
	}
	for sig := range sigs {
		if sig == syscall.SIGTERM || sig == syscall.SIGQUIT || sig == syscall.SIGHUP {
			// The signals that terminate the daemon.
			stopErr := w.OnStop()
			if stopErr != nil {
				exitcode <- 1 // caller should os.Exit(1)
			}
			exitcode <- 0 // caller should os.Exit(0)
		} else if sig == syscall.SIGINT {
			// Unclean shutdown with panic message.
			panic("panic exit")
		} else {
			// uncomment this if you want to see uncaught signals
			// log.Printf("**** caught unchecked signal %v\n", sig)
		}
	}
}
