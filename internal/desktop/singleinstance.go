package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// The single-instance lock is a user-local unix socket. The first process
// listens on it; every later process signals the running instance with its
// launch arguments and exits before opening the database, so there is never
// a second writer. A socket left behind by a crash is taken over.

// instanceLaunch is the message a second instance sends to the first.
type instanceLaunch struct {
	Args []string `json:"args"`
}

// InstanceLock owns the single-instance lock while the app runs.
type InstanceLock struct {
	mu       sync.Mutex
	listener net.Listener
	path     string
	self     os.FileInfo // identifies our socket file at path
	onLaunch func(args []string)
}

// signalInstanceLock hands launchArgs to the instance listening on path and
// reports whether one answered.
func signalInstanceLock(path string, launchArgs []string) bool {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	payload, err := json.Marshal(instanceLaunch{Args: launchArgs})
	if err != nil {
		return false
	}
	if _, err := conn.Write(payload); err != nil {
		return false
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	// Wait until the message was read before exiting.
	_, _ = io.Copy(io.Discard, conn)
	return true
}

// TakeInstanceLock tries to become the running instance at path. When
// another instance already holds the lock, launchArgs are signaled to it
// and TakeInstanceLock reports primary=false: the caller must exit without
// opening the database. onLaunch, which may be nil and replaced later with
// setLaunchHandler, runs in a goroutine for every signal the running
// instance receives while the lock is held.
func TakeInstanceLock(path string, launchArgs []string, onLaunch func(args []string)) (*InstanceLock, bool, error) {
	for attempt := range 5 {
		if signalInstanceLock(path, launchArgs) {
			return nil, false, nil
		}
		// Nobody answers: remove a stale socket left by a crash and take
		// over. If another instance wins the race, Listen fails and the
		// next attempt signals it.
		_ = os.Remove(path)
		listener, err := net.Listen("unix", path)
		if err == nil {
			listener.(*net.UnixListener).SetUnlinkOnClose(false)
			lock := &InstanceLock{listener: listener, path: path}
			lock.self, _ = os.Stat(path)
			lock.setLaunchHandler(onLaunch)
			go lock.accept()
			return lock, true, nil
		}
		// Windows reports WSAEADDRINUSE.
		if !errors.Is(err, syscall.EADDRINUSE) && !errors.Is(err, syscall.Errno(10048)) {
			return nil, false, fmt.Errorf("single-instance lock %s: %w", path, err)
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return nil, false, fmt.Errorf("single-instance lock %s: still contended", path)
}

// setLaunchHandler installs the callback for signals from later instances.
func (l *InstanceLock) setLaunchHandler(fn func(args []string)) {
	l.mu.Lock()
	l.onLaunch = fn
	l.mu.Unlock()
}

// accept delivers every received launch to the handler until Close. A nil
// handler drains the connection so senders are not left waiting.
func (l *InstanceLock) accept() {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			data, err := io.ReadAll(io.LimitReader(conn, 1<<16))
			if err != nil {
				return
			}
			var msg instanceLaunch
			if json.Unmarshal(data, &msg) != nil {
				return
			}
			l.mu.Lock()
			fn := l.onLaunch
			l.mu.Unlock()
			if fn != nil {
				fn(msg.Args)
			}
		}()
	}
}

// Close releases the lock, removing the socket file only if it is still
// ours: an instance that found it stale may have replaced it.
func (l *InstanceLock) Close() error {
	err := l.listener.Close()
	if fi, statErr := os.Stat(l.path); statErr == nil && l.self != nil && os.SameFile(fi, l.self) {
		err = errors.Join(err, os.Remove(l.path))
	}
	return err
}

// instanceLockPath returns a short per-user socket path derived from the
// data directory: unix socket paths are limited to about 100 bytes, and the
// hash keeps different data directories (development, packaged) apart.
func instanceLockPath(dataDir string) string {
	sum := sha256.Sum256([]byte(dataDir))
	file := "myanime-" + strconv.Itoa(os.Getuid()) + "-" + hex.EncodeToString(sum[:6]) + ".sock"
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir()} {
		if dir != "" && len(filepath.Join(dir, file)) < 100 {
			return filepath.Join(dir, file)
		}
	}
	return filepath.Join(os.TempDir(), file)
}
