// Package projectstate provides Git common-state backed project mutation coordination.
package projectstate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

var (
	ErrLockHeld  = errors.New("project state lock held")
	errClaimHeld = errors.New("project state recovery claim held")
)

const claimFileName = "reaper.claim"

// LockPath returns the lock directory path for a Git common-state directory.
func LockPath(commonStateDir string) string {
	return filepath.Join(commonStateDir, "grove", "mutation.lock")
}

// Clock abstracts time for deterministic lock tests.
type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// PIDLiveness reports whether a same-host process is alive.
type PIDLiveness interface{ Alive(pid int) (bool, bool) }

type osPIDLiveness struct{}

func (osPIDLiveness) Alive(pid int) (bool, bool) {
	if pid <= 0 || runtime.GOOS == "windows" {
		return false, false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, true
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true, true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false, true
	}
	return false, false
}

// LockOptions controls lock acquisition.
type LockOptions struct {
	StaleAfter  time.Duration
	RetryDelay  time.Duration
	Clock       Clock
	PIDLiveness PIDLiveness
	Hostname    string
	PID         int
}

// Lock is a caller-owned cross-process mutation lock.
type Lock struct{ dir, nonce string }

type metadata struct {
	Nonce     string    `json:"nonce"`
	Hostname  string    `json:"hostname"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
}

// AcquireLock atomically creates a lock directory and writes owner metadata. It uses no hidden goroutines.
func AcquireLock(ctx context.Context, commonStateDir string, opts LockOptions) (*Lock, error) {
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.PIDLiveness == nil {
		opts.PIDLiveness = osPIDLiveness{}
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = 25 * time.Millisecond
	}
	if opts.Hostname == "" {
		opts.Hostname, _ = os.Hostname()
	}
	if opts.PID == 0 {
		opts.PID = os.Getpid()
	}
	dir := LockPath(commonStateDir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	for {
		nonce, err := newNonce()
		if err != nil {
			return nil, err
		}
		err = os.Mkdir(dir, 0o700)
		if err == nil {
			m := metadata{Nonce: nonce, Hostname: opts.Hostname, PID: opts.PID, CreatedAt: opts.Clock.Now().UTC()}
			if err := writeMeta(dir, m); err != nil {
				_ = os.RemoveAll(dir)
				return nil, err
			}
			return &Lock{dir: dir, nonce: nonce}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if recovered, err := tryRecover(dir, opts); err != nil {
			return nil, err
		} else if recovered {
			continue
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", ErrLockHeld, ctx.Err())
		default:
		}
		if err := opts.Clock.Sleep(ctx, opts.RetryDelay); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrLockHeld, err)
		}
	}
}

// Release removes the lock directory only if owned by this Lock.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	release, err := claim(l.dir)
	if errors.Is(err, errClaimHeld) {
		hostname, _ := os.Hostname()
		opts := LockOptions{Clock: realClock{}, PIDLiveness: osPIDLiveness{}, Hostname: hostname}
		if recovered, recoverErr := recoverAbandonedClaim(l.dir, opts); recoverErr != nil {
			return recoverErr
		} else if recovered {
			release, err = claim(l.dir)
		}
	}
	if err != nil {
		return err
	}
	defer release()
	m, err := readMeta(l.dir)
	if err != nil {
		return err
	}
	if m.Nonce != l.nonce {
		return ErrLockHeld
	}
	return os.RemoveAll(l.dir)
}

func tryRecover(dir string, opts LockOptions) (bool, error) {
	// Capture age before claiming because creating the claim updates the
	// directory mtime.
	dirInfo, dirInfoErr := os.Stat(dir)
	release, err := claim(dir)
	if err != nil {
		if errors.Is(err, errClaimHeld) {
			return recoverAbandonedClaim(dir, opts)
		}
		return false, err
	}
	defer release()

	m, err := readMeta(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("read lock metadata: %w", err)
		}
		return recoverIncomplete(dir, dirInfo, dirInfoErr, opts)
	}
	now := opts.Clock.Now()
	if opts.Hostname != "" && m.Hostname == opts.Hostname {
		if alive, known := opts.PIDLiveness.Alive(m.PID); known {
			if alive {
				return false, nil
			}
			return removeIfOwned(dir, m)
		}
	}
	if opts.StaleAfter > 0 && now.Sub(m.CreatedAt) >= opts.StaleAfter {
		return removeIfOwned(dir, m)
	}
	return false, nil
}

func recoverIncomplete(dir string, info os.FileInfo, statErr error, opts LockOptions) (bool, error) {
	if opts.StaleAfter <= 0 {
		return false, nil
	}
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return true, nil
		}
		return false, statErr
	}
	if opts.Clock.Now().Sub(info.ModTime()) < opts.StaleAfter {
		return false, nil
	}
	if _, err := readMeta(dir); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, os.RemoveAll(dir)
}

func removeIfOwned(dir string, m metadata) (bool, error) {
	cur, err := readMeta(dir)
	if err != nil {
		return false, err
	}
	if cur.Nonce != m.Nonce {
		return false, nil
	}
	return true, os.RemoveAll(dir)
}

func claim(dir string) (func(), error) {
	path := filepath.Join(dir, claimFileName)
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	owner := metadata{Nonce: nonce, Hostname: hostname, PID: os.Getpid(), CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(owner)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, errClaimHeld
	}
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	ok = true
	return func() {
		if current, err := readMetadataFile(path); err == nil && current.Nonce == nonce {
			_ = os.Remove(path)
		}
	}, nil
}

func recoverAbandonedClaim(dir string, opts LockOptions) (bool, error) {
	path := filepath.Join(dir, claimFileName)
	observed, readErr := readMetadataFile(path)
	info, statErr := os.Stat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return true, nil
	}
	if statErr != nil {
		return false, statErr
	}

	stale := false
	if readErr == nil && opts.Hostname != "" && observed.Hostname == opts.Hostname {
		if alive, known := opts.PIDLiveness.Alive(observed.PID); known {
			if alive {
				return false, nil
			}
			stale = true
		}
	}
	if !stale && opts.StaleAfter > 0 {
		createdAt := info.ModTime()
		if readErr == nil {
			createdAt = observed.CreatedAt
		}
		stale = opts.Clock.Now().Sub(createdAt) >= opts.StaleAfter
	}
	if !stale {
		return false, nil
	}

	tombNonce, err := newNonce()
	if err != nil {
		return false, err
	}
	tomb := path + ".stale-" + tombNonce
	if err := os.Rename(path, tomb); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	moved, movedErr := readMetadataFile(tomb)
	expected := readErr != nil && movedErr != nil
	if readErr == nil && movedErr == nil {
		expected = moved.Nonce == observed.Nonce
	}
	if !expected {
		// We raced a replacement claim. Restore the exact moved inode only if the
		// claim path is still absent; never overwrite a newer claimant.
		if err := os.Link(tomb, path); err == nil {
			_ = os.Remove(tomb)
		}
		return false, nil
	}
	if err := os.Remove(tomb); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}

func readMetadataFile(path string) (metadata, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return metadata{}, err
	}
	var m metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return metadata{}, err
	}
	if m.Nonce == "" {
		return metadata{}, errors.New("missing owner nonce")
	}
	return m, nil
}

func writeMeta(dir string, m metadata) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, "owner.json"), b, 0o600)
}
func readMeta(dir string) (metadata, error) {
	b, err := os.ReadFile(filepath.Join(dir, "owner.json"))
	if err != nil {
		return metadata{}, err
	}
	var m metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return metadata{}, err
	}
	if m.Nonce == "" {
		return metadata{}, errors.New("missing owner nonce")
	}
	return m, nil
}
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
