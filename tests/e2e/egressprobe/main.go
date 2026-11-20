// The emulator runs this HTTPS client under the shell UID, outside O Tunnel.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}
	pem, err := os.ReadFile("/data/local/tmp/ovpn-egress-ca.pem")
	roots := x509.NewCertPool()
	if err != nil || !roots.AppendCertsFromPEM(pem) {
		os.Exit(2)
	}
	dialer := &net.Dialer{}
	client := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, os.Args[1])
		},
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("https://www.gstatic.com/ovpn-egress-test")
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64))
	if err != nil || response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	fmt.Print(string(raw))
}
