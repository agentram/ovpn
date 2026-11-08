package main

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/tls"
	"golang.org/x/net/dns/dnsmessage"

	"ovpn/internal/model"
	"ovpn/internal/subscription"
	"ovpn/internal/xraycfg"
)

const (
	mobileHost = "10.0.2.2"
	fixtureSNI = "mobile.fixture.test"
	validID    = "11111111-1111-4111-8111-111111111111"
	invalidID  = "22222222-2222-4222-8222-222222222222"
	fixtureIP  = "192.0.2.88"
	fixtureSID = "0123456789abcdef"
)

func androidFixture(ctx context.Context, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	cert, err := certificates(root)
	if err != nil {
		return err
	}
	// Keep the probe destination offline: only this fixture's tunnel routes
	// the reserved IP to the HTTPS responder.
	hits, err := root.OpenFile("probe-hit", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer hits.Close()
	https := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "www.gstatic.com" && r.URL.Path == "/generate_204" {
			if _, err := hits.Write([]byte{1}); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte("fixture\n"))
	}))
	https.EnableHTTP2 = true
	https.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
	https.StartTLS()
	defer https.Close()
	dns, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer dns.Close()
	go serveDNS(dns)

	realityKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	tlsPort, realityPort, err := ports()
	if err != nil {
		return err
	}
	secret := base64.RawURLEncoding.EncodeToString(realityKey.Bytes())
	public := base64.RawURLEncoding.EncodeToString(realityKey.PublicKey().Bytes())
	endpoint := https.Listener.Addr().String()
	inbound := func(port int, security string, stream map[string]any) map[string]any {
		stream["network"], stream["security"] = "tcp", security
		return map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "vless", "streamSettings": stream,
			"settings": map[string]any{"decryption": "none", "clients": []any{
				map[string]string{"id": validID, "flow": "xtls-rprx-vision"},
			}},
		}
	}
	raw, err := json.Marshal(map[string]any{
		"log": map[string]string{"loglevel": "none", "access": "none", "error": "none"},
		"inbounds": []any{
			inbound(tlsPort, "tls", map[string]any{"tlsSettings": map[string]any{"certificates": []any{
				map[string]string{"certificateFile": filepath.Join(dir, "leaf.pem"), "keyFile": filepath.Join(dir, "leaf.key")},
			}}}),
			inbound(realityPort, "reality", map[string]any{"realitySettings": map[string]any{
				"target": endpoint, "privateKey": secret, "shortIds": []string{fixtureSID},
				"serverNames": []string{fixtureSNI},
			}}),
		},
		"outbounds": []any{
			map[string]string{"protocol": "blackhole"},
			redirect("https", endpoint), redirect("dns", dns.LocalAddr().String()),
		},
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "network": "tcp", "ip": []string{fixtureIP}, "port": "443", "outboundTag": "https"},
			map[string]any{"type": "field", "network": "udp", "ip": []string{"1.1.1.1"}, "port": "53", "outboundTag": "dns"},
		}},
	})
	if err != nil {
		return err
	}
	var config conf.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	built, err := config.Build()
	if err != nil {
		return err
	}
	server, err := core.New(built)
	if err != nil {
		return err
	}
	defer server.Close()
	if err := server.Start(); err != nil {
		return err
	}

	store, err := subscription.Open(filepath.Join(dir, "bundles"))
	if err != nil {
		return err
	}
	defer store.Close()
	link := func(label, profile, id string, port int) subscription.Entry {
		return subscription.Entry{Link: xraycfg.BuildVLESSLink(xraycfg.LinkInput{
			Profile: profile, Address: mobileHost, Port: port, UUID: id, ServerName: fixtureSNI,
			Password: public, ShortID: fixtureSID, Label: label, Fingerprint: "chrome",
		})}
	}
	bundle, _, err := store.Publish("", "fixture", "", []subscription.Entry{
		link("Primary", model.TransportProfileRealityTCPVision, validID, realityPort),
		link("Backup", model.TransportProfileTLSSelfSNIWeb, validID, tlsPort),
		link("Refused", model.TransportProfileTLSSelfSNIWeb, invalidID, tlsPort),
	}, false)
	if err != nil {
		return err
	}
	if err := root.WriteFile("ready", []byte(bundle), 0o600); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func redirect(tag, address string) map[string]any {
	host, _, _ := net.SplitHostPort(address)
	return map[string]any{
		"tag": tag, "protocol": "freedom",
		"settings": map[string]any{"redirect": address, "finalRules": []any{
			map[string]any{"action": "allow", "ip": []string{host}},
		}},
	}
}

func ports() (int, int, error) {
	a, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, 0, err
	}
	defer a.Close()
	b, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, 0, err
	}
	defer b.Close()
	return a.Addr().(*net.TCPAddr).Port, b.Addr().(*net.TCPAddr).Port, nil
}

func serveDNS(socket net.PacketConn) {
	var packet [4096]byte
	for {
		n, remote, err := socket.ReadFrom(packet[:])
		if err != nil {
			return
		}
		var req dnsmessage.Message
		if req.Unpack(packet[:n]) != nil || req.Response || len(req.Questions) != 1 {
			continue
		}
		q := req.Questions[0]
		resp := dnsmessage.Message{Header: dnsmessage.Header{ID: req.ID, Response: true, RecursionAvailable: true}, Questions: req.Questions}
		if !strings.EqualFold(q.Name.String(), "www.gstatic.com.") {
			resp.RCode = dnsmessage.RCodeNameError
		} else if q.Type == dnsmessage.TypeA {
			resp.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET},
				Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 88}}}}
		}
		if data, err := resp.Pack(); err == nil {
			_, _ = socket.WriteTo(data, remote)
		}
	}
}

func certificates(root *os.Root) (tls.Certificate, error) {
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "OVPN test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true, IsCA: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caKey.Public(), caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: fixtureSNI},
		NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, DNSNames: []string{fixtureSNI, "www.gstatic.com"},
		IPAddresses: []net.IP{net.ParseIP(mobileHost), net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, key.Public(), caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	private := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	for name, data := range map[string][]byte{
		"root.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), "leaf.pem": cert, "leaf.key": private,
	} {
		if err := root.WriteFile(name, data, 0o600); err != nil {
			return tls.Certificate{}, fmt.Errorf("write test certificate: %w", err)
		}
	}
	return tls.X509KeyPair(cert, private)
}
