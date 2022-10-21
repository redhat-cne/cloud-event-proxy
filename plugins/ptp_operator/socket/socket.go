package socket

import (
	"net"
	"os"
	"time"
)

var (
	staleSocketTimeout = 100 * time.Millisecond
)

// Listen ... listen to ptp daemon logs
func Listen(addr string) (l net.Listener, e error) {
	uAddr, err := net.ResolveUnixAddr("unix", addr)
	if err != nil {
		return nil, err
	}

	// Try to listen on the socket. If that fails we check to see if it's a stale
	// socket and remove it if it is. Then we try to listen one more time.
	l, err = net.ListenUnix("unix", uAddr)
	if err != nil {
		if err = removeIfStaleUnixSocket(addr); err != nil {
			return nil, err
		}
		if l, err = net.ListenUnix("unix", uAddr); err != nil {
			return nil, err
		}
	}
	return l, err
}

// removeIfStaleUnixSocket takes in a path and removes it iff it is a socket
// that is refusing connections
func removeIfStaleUnixSocket(socketPath string) error {
	// Ensure it's a socket; if not return without an error
	if st, err := os.Stat(socketPath); err != nil || st.Mode()&os.ModeType != os.ModeSocket {
		return nil
	}
	// Try to connect
	conn, err := net.DialTimeout("unix", socketPath, staleSocketTimeout)
	if err != nil { // =syscall.ECONNREFUSED {
		return os.Remove(socketPath)
	} else if err == nil {
		return conn.Close()
	}
	return nil
}
