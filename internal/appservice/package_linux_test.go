package appservice

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestPackageRemovalJoinsManagedAdmissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed")
	transaction, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Close()
	if err := transaction.Chmod(0644); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Geteuid())
	ctx, release, err := watchPackageMarker(t.Context(), path, uid)
	if err != nil {
		t.Fatal(err)
	}
	finish := sync.OnceFunc(release)
	defer finish()
	if err := os.Chtimes(path, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("metadata change retired service: %v", err)
	}

	// A service whose admission races unlink must either observe retirement
	// or fail to acquire the marker. No stale process can cross reinstall.
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			peer, finish, err := watchPackageMarker(t.Context(), path, uid)
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			if err != nil {
				t.Errorf("unexpected admission error: %v", err)
				return
			}
			defer finish()
			select {
			case <-peer.Done():
			case <-time.After(3 * time.Second):
				t.Error("racing admission survived unlink")
			}
		})
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("running service did not observe unlink")
	}
	group.Wait()
	if err := syscall.Flock(int(transaction.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("removal failed to wait for cleanup: %v", err)
	}
	if _, _, err := watchPackageMarker(t.Context(), path, uid); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new admission after unlink: %v", err)
	}
	finish()
	if err := syscall.Flock(int(transaction.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("clean service still blocked removal: %v", err)
	}
}

func TestPackageAdmissionRequiresTrustedMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker")
	uid := uint32(os.Geteuid())
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	reject := func(owner uint32) {
		t.Helper()
		if _, release, err := watchPackageMarker(t.Context(), path, owner); err == nil {
			release()
			t.Fatal("admitted a process with invalid marker")
		}
	}
	reject(uid)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	reject(uid + 1)
	if err := os.Link(path, path+".extra"); err != nil {
		t.Fatal(err)
	}
	reject(uid)
}
