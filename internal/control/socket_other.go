//go:build !linux && !darwin

package control

import (
	"fmt"
	"net"
)

type Listener struct{ net.Listener }

func SocketPath(string) (string, error) { return "", unsupported() }
func Listen(string) (*Listener, error)  { return nil, unsupported() }
func validateSocket(string) error       { return unsupported() }
func unsupported() error {
	return fmt.Errorf("Panora service sockets currently require Linux or macOS")
}
