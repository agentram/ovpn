package androidbridge

import "golang.org/x/sys/unix"

// InterfaceName identifies the service-owned TUN for VPN-bound health checks.
func InterfaceName(fd int32) string {
	ifr, err := unix.NewIfreq("")
	if err != nil || unix.IoctlIfreq(int(fd), unix.TUNGETIFF, ifr) != nil {
		return ""
	}
	return ifr.Name()
}
