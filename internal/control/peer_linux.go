package control

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
)

// Peer identifies the process at the other end of a Linux control connection.
type Peer struct {
	PID int
	UID uint32
}

// StatusWithPeer reads the API and credentials on the same connection. A live
// peer can be returned with an error if the peer's API is unavailable.
func StatusWithPeer(ctx context.Context, path string) (Status, Peer, error) {
	var status Status
	peers := make(chan Peer, 1)
	err := requestBodyWithDial(ctx, path, http.MethodGet, "/v1/status", nil, &status, 1<<20, func(ctx context.Context, path string) (net.Conn, error) {
		connection, peer, err := dialPeer(ctx, path)
		if err == nil {
			select {
			case peers <- peer:
			default:
			}
		}
		return connection, err
	})
	select {
	case peer := <-peers:
		return status, peer, err
	default:
		return status, Peer{}, err
	}
}

// ShutdownPeer refuses to send a shutdown request to a replacement process.
func ShutdownPeer(ctx context.Context, path string, expected Peer) (ShutdownResult, error) {
	var result ShutdownResult
	err := requestBodyWithDial(ctx, path, http.MethodPost, "/v1/shutdown", nil, &result, 1<<20, func(ctx context.Context, path string) (net.Conn, error) {
		connection, peer, err := dialPeer(ctx, path)
		if err != nil {
			return nil, err
		}
		if peer != expected {
			connection.Close()
			return nil, fmt.Errorf("control socket peer changed before shutdown")
		}
		return connection, nil
	})
	return result, err
}

func dialPeer(ctx context.Context, path string) (net.Conn, Peer, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, Peer{}, err
	}
	peer, err := unixPeer(connection.(*net.UnixConn))
	if err != nil || peer.UID != uint32(os.Geteuid()) || peer.PID <= 0 {
		connection.Close()
		if err != nil {
			return nil, Peer{}, fmt.Errorf("read control socket peer: %w", err)
		}
		return nil, Peer{}, fmt.Errorf("control socket peer is outside this user session")
	}
	return connection, peer, nil
}

func unixPeer(connection *net.UnixConn) (Peer, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var credentials *syscall.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, socketErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return Peer{}, err
	}
	if socketErr != nil {
		return Peer{}, socketErr
	}
	return Peer{PID: int(credentials.Pid), UID: credentials.Uid}, nil
}
