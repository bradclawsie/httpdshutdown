package httpdshutdown

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func init() {
	fmt.Printf("\n\nNote: these tests include some time delays\n\n\n")
}

func TestNil(t *testing.T) {
	var w *Watcher
	w = nil
	err := w.OnStop()
	if err == nil {
		t.Errorf("TestNil: should have error")
	}
}

func TestBadTimeout(t *testing.T) {
	_, wErr := NewWatcher(-1)
	if wErr == nil {
		t.Errorf("TestBadTimeout: should have error")
	}
}

func TestValid(t *testing.T) {
	w, wErr := NewWatcher(3000)
	if w == nil || wErr != nil {
		t.Errorf("TestValid: should not be nil")
	}
	err := w.OnStop()
	if err != nil {
		t.Errorf("TestValid: should not have error")
	}
}

func sampleShutdownHook() error {
	fmt.Println("shutdown hook called")
	return nil
}

func TestStop(t *testing.T) {
	w, wErr := NewWatcher(3000, sampleShutdownHook)
	if w == nil || wErr != nil {
		t.Errorf("TestStop: should not be nil")
	}
	w.RecordConnState(http.StateNew)
	err := w.OnStop()
	if err == nil {
		t.Errorf("TestStop: should have error from 1 second timeout to force stop")
	}
	w.RecordConnState(http.StateClosed)
	err = w.OnStop()
	if err != nil {
		t.Errorf("TestStop: should not have an error")
	}
	w.RecordConnState(http.StateNew)
	w.RecordConnState(http.StateHijacked)
	err = w.OnStop()
	if err != nil {
		t.Errorf("TestStop: should not have an error")
	}
}

func TestHttpDaemonTimeout(t *testing.T) {
	fmt.Printf("\n\n")
	w, wErr := NewWatcher(2000, sampleShutdownHook)
	if w == nil || wErr != nil {
		t.Errorf("TestHttpDaemonTimeout: should not be nil")
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("hello from the test daemon; sleeping")
		time.Sleep(5 * time.Second)
		fmt.Println("goodbye from the test daemon")
		fmt.Fprintln(w, "Hello, client")
		return
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(handler))

	ts.Config.ConnState = func(conn net.Conn, newState http.ConnState) {
		fmt.Printf("(0) NEW CONN STATE:%v\n", newState)
		w.RecordConnState(newState)
		return
	}
	// needed to force the connection to close
	ts.Config.ReadTimeout = 8 * time.Second
	ts.Config.WriteTimeout = 8 * time.Second

	ts.Start()
	defer ts.Close()

	var wg sync.WaitGroup

	fmt.Println("watcher should trigger before handler completes: should see a timeout message")

	wg.Add(1)
	go func() {
		fmt.Println("about to call handler")
		getResp, getErr := http.Get(ts.URL)
		if getErr != nil {
			t.Errorf(getErr.Error())
		}
		_, readErr := ioutil.ReadAll(getResp.Body)
		getResp.Body.Close()
		if readErr != nil {
			t.Errorf(readErr.Error())
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		fmt.Println("about to call OnStop, sleep 1 first")
		time.Sleep(1 * time.Second)
		err := w.OnStop()
		if err == nil {
			t.Errorf("TestHttpDaemonTimeout: should have an error, a timeout was supposed to occur")
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestHttpDaemonNormalExit(t *testing.T) {
	fmt.Printf("\n\n")
	w, wErr := NewWatcher(20000, sampleShutdownHook)
	if w == nil || wErr != nil {
		t.Errorf("TestHttpDaemonNormalExit: should not be nil")
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("hello from the test daemon; sleeping")
		time.Sleep(1 * time.Second)
		fmt.Println("goodbye from the test daemon")
		fmt.Fprintln(w, "Hello, client")
		return
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(handler))

	ts.Config.ConnState = func(conn net.Conn, newState http.ConnState) {
		fmt.Printf("(1) NEW CONN STATE:%v\n", newState)
		w.RecordConnState(newState)
		return
	}
	// needed to force the connection to close
	ts.Config.ReadTimeout = 3 * time.Second
	ts.Config.WriteTimeout = 3 * time.Second

	ts.Start()
	defer ts.Close()

	var wg sync.WaitGroup

	fmt.Println("watcher should not trigger before handler completes: should see no timeout message")

	wg.Add(1)
	go func() {
		fmt.Println("about to call handler")
		getResp, getErr := http.Get(ts.URL)
		if getErr != nil {
			t.Errorf(getErr.Error())
		}
		_, readErr := ioutil.ReadAll(getResp.Body)
		getResp.Body.Close()
		if readErr != nil {
			t.Errorf(readErr.Error())
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		fmt.Println("about to call OnStop, sleep 1 first")
		time.Sleep(1 * time.Second)
		err := w.OnStop()
		if err != nil {
			t.Errorf("TestHttpDaemonNormalExit: should have no error, no timeout was supposed to occur")
		}
		wg.Done()
	}()

	wg.Wait()
}
