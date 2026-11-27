package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

type Hooks struct {
	WatchDesktop func(context.Context, func(), func()) error
	// Ready runs once the listener and HTTP handler can accept requests.
	Ready func(context.Context) error
	// RunCompanion runs with the ready service and is joined during shutdown.
	RunCompanion func(context.Context, string)
	// PackageReplacement reports a linked replacement for the managed service.
	PackageReplacement <-chan string
}

// PackageReplacement tells the caller it can exec the replacement after Run
// has released the listener and playback state.
type PackageReplacement struct{ Executable string }

func (*PackageReplacement) Error() string { return "Panora package was replaced" }

// Run wires the domain service to its HTTP adapter. Playback owns state/scheduling;
// control owns the private socket. Neither package depends on Cobra.
func Run(ctx context.Context, options playback.ServiceOptions, resolve playback.Resolver, socket string, out, errOut io.Writer, hooks Hooks) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, _, err := PrepareConfig(options.Request.ConfigPath, out, false)
	if err != nil {
		return err
	}
	options.Request.ConfigPath = path
	options.Request.SourceLogger = slog.New(slog.NewTextHandler(errOut, nil))
	serviceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	service, err := playback.OpenService(serviceCtx, options, resolve)
	if err != nil {
		return err
	}
	defer service.Close()
	listener, err := control.Listen(socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if _, err := fmt.Fprintf(out, "Panora service listening on %s (interval: %s). Press Ctrl-C to stop.\n", socket, service.Status().Interval); err != nil {
		return err
	}
	service.StartDiscovery()
	service.StartLookahead()
	requested := make(chan struct{})
	var requestOnce sync.Once
	requestStop := func() { requestOnce.Do(func() { close(requested) }) }
	server := &http.Server{Handler: control.HandlerWithShutdown(service, requestStop), BaseContext: func(net.Listener) context.Context { return serviceCtx }, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	if hooks.Ready != nil {
		if err := hooks.Ready(serviceCtx); err != nil {
			cancel()
			_ = server.Close()
			<-serverDone
			return fmt.Errorf("report service readiness: %w", err)
		}
	}
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		service.Run(func(err error) { fmt.Fprintf(errOut, "Rotation attempt failed: %v\n", err) })
	}()
	desktopDone := make(chan struct{})
	go func() {
		defer close(desktopDone)
		if hooks.WatchDesktop != nil {
			runDesktopEvents(serviceCtx, hooks.WatchDesktop, service.RefreshDesktop, service.SetDesktopWarning, errOut)
		}
	}()
	companionDone := make(chan struct{})
	go func() {
		defer close(companionDone)
		if hooks.RunCompanion != nil {
			hooks.RunCompanion(serviceCtx, service.Status().InstanceID)
		}
	}()
	var result error
	var replacement string
	select {
	case <-ctx.Done():
	case <-requested:
	case replacement = <-hooks.PackageReplacement:
	case result = <-service.Errors():
	case result = <-serverDone:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	}
	cancel()
	<-schedulerDone
	<-desktopDone
	<-companionDone
	service.Stop()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		server.Close()
		if result == nil {
			result = err
		}
	}
	if result == nil && replacement != "" && ctx.Err() == nil {
		select {
		case <-requested:
			// A Stop acknowledged during shutdown takes priority over the upgrade.
		default:
			return &PackageReplacement{Executable: replacement}
		}
	}
	return result
}
