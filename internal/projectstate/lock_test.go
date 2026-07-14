package projectstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		c.now = c.now.Add(d)
		return nil
	}
}

type fakeLive struct{ alive, known bool }

func (f fakeLive) Alive(int) (bool, bool) { return f.alive, f.known }

func TestLockMutualExclusionAndCancellation(t *testing.T) {
	dir := t.TempDir()
	l, err := AcquireLock(context.Background(), dir, LockOptions{Hostname: "h", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = AcquireLock(ctx, dir, LockOptions{Hostname: "h", PID: 2})
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("got %v, want held", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := AcquireLock(context.Background(), dir, LockOptions{Hostname: "h", PID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := l2.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockStaleAndDeadOwnerRecovery(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{now: time.Unix(1000, 0)}
	l, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 1, PIDLiveness: fakeLive{alive: true, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(2 * time.Hour)
	blockedCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireLock(blockedCtx, dir, LockOptions{Clock: c, Hostname: "h", PID: 2, StaleAfter: time.Hour, PIDLiveness: fakeLive{alive: true, known: true}}); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("live same-host lock was stolen after TTL: %v", err)
	}
	l2, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 2, StaleAfter: time.Hour, PIDLiveness: fakeLive{alive: false, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("old owner released new lock: %v", err)
	}
	if err := l2.Release(); err != nil {
		t.Fatal(err)
	}
	l3, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 3})
	if err != nil {
		t.Fatal(err)
	}
	l4, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 4, PIDLiveness: fakeLive{alive: false, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := l3.Release(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("old owner released recovered lock: %v", err)
	}
	if err := l4.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteStaleLockRecovery(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{now: time.Unix(1000, 0)}
	old, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "host-a", PID: 1, PIDLiveness: fakeLive{alive: true, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(2 * time.Hour)
	replacement, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "host-b", PID: 2, StaleAfter: time.Hour, PIDLiveness: fakeLive{alive: true, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Release(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("old remote owner released replacement lock: %v", err)
	}
	if err := replacement.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestOldReleaseDoesNotRemoveReplacementOwner(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{now: time.Unix(1000, 0)}
	old, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 1, PIDLiveness: fakeLive{alive: true, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(2 * time.Hour)
	replacement, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 2, StaleAfter: time.Hour, PIDLiveness: fakeLive{alive: false, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Release(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("old release = %v, want held", err)
	}
	if _, err := os.Stat(LockPath(dir)); err != nil {
		t.Fatalf("replacement lock removed: %v", err)
	}
	if err := replacement.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerReleaseRecoversDeadSameHostClaim(t *testing.T) {
	dir := t.TempDir()
	owner, err := AcquireLock(context.Background(), dir, LockOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hostname, _ := os.Hostname()
	claimOwner := metadata{Nonce: "dead-claim", Hostname: hostname, PID: 1 << 30, CreatedAt: time.Now()}
	data, err := json.Marshal(claimOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(LockPath(dir), claimFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("owner could not recover dead reaper claim: %v", err)
	}
}

func TestAbandonedRecoveryClaimIsReapedAfterStaleThreshold(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{now: time.Now()}
	old, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "owner-host", PID: 1, PIDLiveness: fakeLive{alive: true, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claim(LockPath(dir)); err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(3 * time.Hour)
	replacement, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "other-host", PID: 2, StaleAfter: 2 * time.Hour, PIDLiveness: fakeLive{alive: true, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Release(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("old owner released replacement: %v", err)
	}
	if err := replacement.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestSerializedReaperPreservesClaimedLock(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{now: time.Unix(1000, 0)}
	l, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 1, PIDLiveness: fakeLive{alive: true, known: true}})
	if err != nil {
		t.Fatal(err)
	}
	releaseClaim, err := claim(LockPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(2 * time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = AcquireLock(ctx, dir, LockOptions{Clock: c, Hostname: "h", PID: 2, StaleAfter: time.Hour, PIDLiveness: fakeLive{alive: false, known: true}})
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("got %v, want held", err)
	}
	if _, err := os.Stat(filepath.Join(LockPath(dir), "owner.json")); err != nil {
		t.Fatalf("claimed owner removed: %v", err)
	}
	releaseClaim()
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestStaleIncompleteLockIsConservativeThenRecoverable(t *testing.T) {
	dir := t.TempDir()
	lockDir := LockPath(dir)
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	c := &fakeClock{now: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := AcquireLock(ctx, dir, LockOptions{Clock: c, Hostname: "h", PID: 2, StaleAfter: time.Hour})
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("got %v, want held", err)
	}
	c.now = time.Now().Add(2 * time.Hour)
	l, err := AcquireLock(context.Background(), dir, LockOptions{Clock: c, Hostname: "h", PID: 2, StaleAfter: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockFailsConservativelyWhenOwnerUnknown(t *testing.T) {
	dir := t.TempDir()
	l, err := AcquireLock(context.Background(), dir, LockOptions{Hostname: "h", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = AcquireLock(ctx, dir, LockOptions{Hostname: "h", PID: 2, PIDLiveness: fakeLive{alive: false, known: false}})
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("got %v, want held", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}
