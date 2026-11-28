package playback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
)

type lookaheadTestBackend struct {
	started chan struct{}
	finish  chan error
	calls   atomic.Int32
}

func (*lookaheadTestBackend) Probe(context.Context) error { return nil }

func (b *lookaheadTestBackend) SetWallpaper(ctx context.Context, _ string, _ desktop.DisplayOptions) error {
	if b.calls.Add(1) == 1 {
		return nil
	}
	select {
	case b.started <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-b.finish:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lookaheadFixture(t *testing.T, backend desktop.WallpaperBackend) (*Service, Status) {
	t.Helper()
	s, root := testService(t, backend)
	writeScanImage(t, filepath.Join(root, "b.png"), 3, 2)
	writeScanImage(t, filepath.Join(root, "c.png"), 4, 2)
	return s, act(t, s, "next")
}

func awaitLookahead(t *testing.T, s *Service, ready func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status := s.Status()
		if ready(status) {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("lookahead did not reach expected state: %+v", status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLookaheadUsesTwoWarmedChoicesAcrossDelayedNext(t *testing.T) {
	backend := &lookaheadTestBackend{started: make(chan struct{}), finish: make(chan error)}
	s, first := lookaheadFixture(t, backend)
	began := time.Now()
	s.StartLookahead()
	ready := awaitLookahead(t, s, func(status Status) bool { return status.PreparedNextImageID != 0 })
	t.Logf("first prepared preview after %v", time.Since(began))
	if !ready.Paused || ready.NextAt != nil || ready.Current.ID != first.Current.ID || ready.HistoryLength != 1 || backend.calls.Load() != 1 {
		t.Fatalf("warmup changed accepted playback: %+v", ready)
	}
	var secondID int64
	awaitLookahead(t, s, func(Status) bool {
		s.actionMu.Lock()
		defer s.actionMu.Unlock()
		if len(s.lookahead.choices) < 2 {
			return false
		}
		secondID = s.lookahead.choices[1].identity.ImageID
		return true
	})
	if secondID == first.Current.ImageID || secondID == ready.PreparedNextImageID {
		t.Fatalf("prepared second slot repeated an existing image: %d", secondID)
	}
	preview, err := s.Preview(t.Context(), secondID)
	if err != nil || !preview.Available {
		t.Fatalf("second slot not warm before dispatch: %+v %v", preview, err)
	}
	for i, want := range []int64{ready.PreparedNextImageID, secondID} {
		result := make(chan error, 1)
		go func() {
			_, err := s.Action(t.Context(), "next")
			result <- err
		}()
		select {
		case <-backend.started:
		case err := <-result:
			t.Fatalf("Next failed before dispatch: %v", err)
		case <-time.After(time.Second):
			t.Fatal("Next did not reach the setter")
		}
		pending := s.Status()
		if !pending.Busy || pending.PendingImageID != want || pending.HistoryLength != i+1 || pending.Current.ImageID == want {
			t.Fatalf("accepted playback changed before acknowledgement: %+v", pending)
		}
		if _, err := s.Action(t.Context(), "next"); err == nil {
			t.Fatal("another Next dispatched while the setter was pending")
		}
		backend.finish <- nil
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		accepted := s.Status()
		if accepted.Current.ImageID != want || accepted.HistoryLength != i+2 || accepted.PendingImageID != 0 || !accepted.Paused {
			t.Fatalf("unexpected accepted play after dispatch: %+v", accepted)
		}
		if i == 0 && accepted.PreparedNextImageID != secondID {
			t.Fatalf("warmed second slot was not promoted: %+v", accepted)
		}
	}
}

func TestLookaheadKeepsTenDistinctChoicesAndWarmsOnlyFive(t *testing.T) {
	s, root := testService(t, &fakeWallpaperBackend{})
	for i := range 12 {
		writeScanImage(t, filepath.Join(root, fmt.Sprintf("future-%d.png", i)), 3+i, 2)
	}
	first := act(t, s, "next")
	s.StartLookahead()
	var ready []playbackChoice
	awaitLookahead(t, s, func(Status) bool {
		s.actionMu.Lock()
		defer s.actionMu.Unlock()
		if len(s.lookahead.choices)+len(s.lookahead.planned) != 10 {
			return false
		}
		seen := map[int64]bool{first.Current.ImageID: true}
		for i, choice := range append(append([]playbackChoice{}, s.lookahead.choices...), s.lookahead.planned...) {
			if seen[choice.identity.ImageID] || (choice.path != "") != (i < 5) {
				t.Fatalf("duplicate or incorrectly cached choice at slot %d: %+v", i, choice)
			}
			seen[choice.identity.ImageID] = true
		}
		ready = append(ready, s.lookahead.choices...)
		return true
	})
	for i, choice := range ready {
		status, err := s.Action(t.Context(), "next")
		if err != nil || status.Current == nil || status.Current.ImageID != choice.identity.ImageID || !status.Paused {
			t.Fatalf("Next did not consume warmed slot %d: %+v %v", i, status, err)
		}
	}
}

func TestLookaheadPrioritizesForwardHistoryAndPreservesAcceptedOnFailure(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, first := lookaheadFixture(t, backend)
	second := act(t, s, "next")
	act(t, s, "previous")
	s.StartLookahead()
	ready := awaitLookahead(t, s, func(status Status) bool { return status.PreparedNextImageID != 0 })
	if ready.PreparedNextImageID != second.Current.ImageID || ready.Current.ID != first.Current.ID || ready.HistoryLength != 2 || backend.calls != 3 {
		t.Fatalf("unexpected forward lookahead: %+v", ready)
	}
	backend.err = errors.New("setter rejected")
	failed, err := s.Action(t.Context(), "next")
	if err == nil || !failed.Uncertain || failed.PendingImageID != 0 || failed.PreparedNextImageID != 0 || failed.Current.ID != first.Current.ID || failed.HistoryCursor != 0 ||
		failed.HistoryLength != 2 || backend.calls != 4 {
		t.Fatalf("failure changed accepted playback: %+v %v", failed, err)
	}
}

type gatedLookaheadProvider struct {
	source.Provider
	resolving chan struct{}
	release   chan struct{}
}

func (p gatedLookaheadProvider) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	select {
	case p.resolving <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return p.Provider.Resolve(ctx, params)
	case <-ctx.Done():
		return source.Resolved{}, ctx.Err()
	}
}

type secondLookaheadGate struct {
	gatedLookaheadProvider
	calls atomic.Int32
}

func (p *secondLookaheadGate) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	if p.calls.Add(1) == 1 {
		return p.Provider.Resolve(ctx, params)
	}
	return p.gatedLookaheadProvider.Resolve(ctx, params)
}

func TestLookaheadAdvertisesFirstWhileSecondIsStalled(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, first := lookaheadFixture(t, backend)
	gate := &secondLookaheadGate{gatedLookaheadProvider: gatedLookaheadProvider{
		Provider: s.request.Source, resolving: make(chan struct{}, 1), release: make(chan struct{}),
	}}
	s.request.Source = gate
	s.StartLookahead()
	select {
	case <-gate.resolving:
	case <-time.After(2 * time.Second):
		t.Fatal("second warmup did not start")
	}
	ready := s.Status()
	if ready.PreparedNextImageID == 0 || ready.Current.ID != first.Current.ID {
		t.Fatalf("visible lookahead did not publish before the second slot finished: %+v", ready)
	}
	if preview, err := s.Preview(t.Context(), ready.PreparedNextImageID); err != nil || !preview.Available {
		t.Fatalf("advertised first thumbnail was not readable: %+v %v", preview, err)
	}
	close(gate.release)
	after := awaitLookahead(t, s, func(status Status) bool {
		return status.PreparedBeyond != nil
	})
	if after.PreparedBeyond.ImageID == ready.PreparedNextImageID {
		t.Fatalf("second lookahead duplicated the first: %+v", after)
	}
	if after.PreparedNextImageID != ready.PreparedNextImageID || after.PreparedNextRevision != ready.PreparedNextRevision {
		t.Fatalf("completing the second slot invalidated the visible thumbnail: before=%+v after=%+v", ready, after)
	}
}

func TestLookaheadDiscardsChoiceWhenRulesChangeDuringPreparation(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, first := lookaheadFixture(t, backend)
	gate := gatedLookaheadProvider{Provider: s.request.Source, resolving: make(chan struct{}, 1), release: make(chan struct{})}
	s.request.Source = gate
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.prepareLookahead()
	}()
	select {
	case <-gate.resolving:
	case <-time.After(time.Second):
		t.Fatal("warmup did not attempt materialization")
	}
	data, err := os.ReadFile(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "min_width = 2", "min_width = 20", 1)
	if updated == string(data) {
		t.Fatal("fixture did not edit rules")
	}
	if err := os.WriteFile(s.request.ConfigPath, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	close(gate.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("warmup did not finish")
	}
	if status := s.Status(); status.PreparedNextImageID != 0 || status.Current.ID != first.Current.ID || backend.calls != 1 {
		t.Fatalf("stale warmup was published after a rule change: %+v", status)
	}
}
