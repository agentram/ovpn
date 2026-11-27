package integrations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/clientcfg"
	"ovpn/internal/probe"
	"ovpn/internal/probequeue"
	"ovpn/internal/version"

	metricdto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const TargetURL = "https://www.gstatic.com/generate_204"
const Timeout = 20 * time.Second

type Tool struct {
	Engine  string `json:"engine"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}
type Input struct {
	Config   clientcfg.Config
	Link     string
	Revision string
	Key      string
}
type Runner struct {
	TargetURL string
	WorkDir   string
}

func Version(engine string) string {
	if engine == "checker" {
		return "1.3.1"
	}
	if engine == "knife" || strings.HasPrefix(engine, "knife-") {
		return "11.5.0"
	}
	return version.Current() + " / Xray " + probe.CoreVersion()
}
func toolEngine(engine string) string {
	if strings.HasPrefix(engine, "knife-") {
		return "knife"
	}
	return engine
}

func Token() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(b[:])
}

func environment(dir string) []string {
	result := []string{"HOME=" + dir, "USERPROFILE=" + dir, "XDG_CONFIG_HOME=" + dir, "XDG_CACHE_HOME=" + dir, "TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir}
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		result = append(result, "SystemRoot="+root, "PATH="+filepath.Join(root, "System32"))
	} else {
		result = append(result, "PATH=/usr/bin:/bin")
	}
	return result
}

func verifyHash(tool Tool) error {
	if !filepath.IsAbs(tool.Path) || tool.Engine != "checker" && tool.Engine != "knife" || len(tool.SHA256) != 64 {
		return errors.New("TOOL_INVALID")
	}
	info, err := os.Lstat(tool.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return errors.New("TOOL_UNAVAILABLE")
	}
	f, err := os.Open(tool.Path)
	if err != nil {
		return errors.New("TOOL_UNAVAILABLE")
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.New("TOOL_UNAVAILABLE")
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(tool.SHA256) {
		return errors.New("TOOL_HASH_MISMATCH")
	}
	return nil
}

type limitedOutput struct{ data []byte }

func (w *limitedOutput) Write(b []byte) (int, error) {
	if len(w.data)+len(b) > 8192 {
		return 0, errors.New("tool output limit")
	}
	w.data = append(w.data, b...)
	return len(b), nil
}

func Verify(ctx context.Context, tool Tool) (Tool, error) {
	if err := verifyHash(tool); err != nil {
		return Tool{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "ovpn-tool-version-*")
	if err != nil {
		return Tool{}, errors.New("TOOL_UNAVAILABLE")
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, tool.Path, "--version")
	cmd.Env = environment(dir)
	cmd.Dir = dir
	var out limitedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if cmd.Run() != nil {
		return Tool{}, errors.New("TOOL_VERSION_UNSUPPORTED")
	}
	text := string(out.data)
	expected := Version(tool.Engine)
	valid := false
	for _, field := range strings.Fields(text) {
		if strings.TrimPrefix(field, "v") == expected {
			valid = true
		}
	}
	if !valid {
		return Tool{}, errors.New("TOOL_VERSION_UNSUPPORTED")
	}
	tool.Version = expected
	return tool, nil
}

func (r Runner) Run(ctx context.Context, engine string, tool Tool, input Input) dto.Observation {
	result := dto.Observation{Source: engine, EngineVersion: Version(engine), Revision: input.Revision, Outcome: "observer_error", FailureClass: "observer", Code: "TOOL_UNAVAILABLE", Bytes: -1, Unverified: []string{"runtime", "udp", "dns", "ipv6", "gui-import", "wire-bytes"}}
	if engine != "native" {
		release, err := probequeue.Shared.Acquire(ctx, input.Config)
		if err != nil {
			result.Code = "CHECK_QUEUE_BUSY"
			return result
		}
		defer release()
	}
	target := r.TargetURL
	if target == "" {
		target = TargetURL
	}
	start := time.Now()
	if engine == "native" {
		opts := probe.Options{URL: target, ExpectedStatus: 204, Timeout: Timeout, MaxBodyBytes: 4096}
		p, err := probe.CheckConfig(ctx, input.Config, opts)
		result.DurationMS, result.TTFBMS, result.Bytes, result.StatusCode = p.DurationMS, p.TTFBMS, p.Bytes, p.StatusCode
		if p.Success {
			result.Outcome = "passed"
			result.FailureClass = ""
			result.Code = ""
		} else if p.ErrorCode != "" {
			result.Outcome = "failed"
			result.FailureClass = "traffic"
			result.Code = p.ErrorCode
		} else if err != nil {
			result.Code = "CORE_UNAVAILABLE"
		}
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if tool.Engine != toolEngine(engine) {
		return result
	}
	if err := verifyHash(tool); err != nil {
		result.Code = err.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if r.WorkDir != "" {
		if err := os.MkdirAll(r.WorkDir, 0o700); err != nil {
			result.Code = "PRIVATE_STORAGE_UNAVAILABLE"
			return result
		}
	}
	dir, err := os.MkdirTemp(r.WorkDir, "job-*")
	if err != nil {
		result.Code = "PRIVATE_STORAGE_UNAVAILABLE"
		return result
	}
	defer os.RemoveAll(dir)
	switch engine {
	case "checker":
		result = r.checker(ctx, tool, input, dir, target, result)
	case "knife-xray", "knife-singbox":
		result = r.knife(ctx, tool, input, dir, target, engine, result)
	}
	result.DurationMS = time.Since(start).Milliseconds()
	if ctx.Err() != nil {
		result.Outcome = "observer_error"
		result.FailureClass = "observer"
		result.Code = "TOOL_DEADLINE"
	}
	return result
}

func run(ctx context.Context, tool Tool, dir string, args []string) error {
	cmd := exec.CommandContext(ctx, tool.Path, args...)
	cmd.Dir = dir
	cmd.Env = environment(dir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return errors.New("TOOL_EXECUTION_FAILED")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				return errors.New("TOOL_EXECUTION_FAILED")
			}
			return nil
		case <-ticker.C:
			var bytes int64
			files := 0
			err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				files++
				if files > 1000 {
					return errors.New("limit")
				}
				if entry.Type().IsRegular() {
					info, err := entry.Info()
					if err != nil {
						return err
					}
					bytes += info.Size()
					if bytes > 64<<20 || filepath.Base(path) == "result.jsonl" && info.Size() > 1<<20 {
						return errors.New("limit")
					}
				}
				return nil
			})
			if err != nil {
				_ = cmd.Process.Kill()
				<-done
				return errors.New("TOOL_OUTPUT_LIMIT")
			}
		}
	}
}

func (r Runner) checker(ctx context.Context, tool Tool, input Input, dir, target string, result dto.Observation) dto.Observation {
	// No geodata rules are generated. Empty protobuf lists prevent the upstream
	// startup helper from downloading unrelated geodata for this one-shot check.
	for _, sub := range []string{"geo", "profiles"} {
		if os.Mkdir(filepath.Join(dir, sub), 0o700) != nil {
			return result
		}
	}
	for _, file := range []string{"geoip.dat", "geosite.dat"} {
		if os.WriteFile(filepath.Join(dir, "geo", file), nil, 0o600) != nil {
			return result
		}
	}
	cfg := input.Config
	cfg.Remarks = input.Key
	raw, err := json.Marshal(cfg)
	if err != nil {
		return result
	}
	if os.WriteFile(filepath.Join(dir, "profiles", "profile.json"), raw, 0o600) != nil {
		return result
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		result.Code = "LOOPBACK_UNAVAILABLE"
		return result
	}
	defer listener.Close()
	nonce := Token()
	var mu sync.Mutex
	var received bool
	var passed bool
	var latency int64
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/"+nonce {
			http.NotFound(w, req)
			return
		}
		if req.Method == http.MethodGet {
			_, _ = io.WriteString(w, "127.0.0.1")
			return
		}
		if req.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 64<<10))
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		ok, ms, err := ParseChecker(body, input.Key)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received, passed, latency = true, ok, ms
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	callback := "http://" + listener.Addr().String() + "/" + nonce
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return result
	}
	port := portListener.Addr().(*net.TCPAddr).Port
	_ = portListener.Close()
	args := []string{"--run-once", "--subscription-url", "folder://" + filepath.Join(dir, "profiles"), "--subscription-update=false", "--proxy-check-method=status", "--proxy-status-check-url", target, "--proxy-ip-check-url", callback, "--metrics-push-url", callback, "--proxy-timeout", "20", "--proxy-check-concurrency", "1", "--simulate-latency=false", "--proxy-resolve-domains=false", "--xray-start-port", strconv.Itoa(port), "--log-level", "none", "--xray-log-level", "none"}
	if err := run(ctx, tool, dir, args); err != nil {
		result.Code = err.Error()
		return result
	}
	mu.Lock()
	defer mu.Unlock()
	if !received {
		result.Code = "TOOL_RESULT_MISSING"
		return result
	}
	result.Outcome = "failed"
	result.FailureClass = "traffic"
	result.Code = "HTTPS_CHECK_FAILED"
	result.TTFBMS = latency
	if passed {
		result.Outcome = "passed"
		result.FailureClass = ""
		result.Code = ""
		result.StatusCode = 204
	}
	return result
}

func ParseChecker(raw []byte, name string) (bool, int64, error) {
	if len(raw) > 64<<10 {
		return false, 0, errors.New("TOOL_RESULT_LIMIT")
	}
	decoder := expfmt.NewDecoder(strings.NewReader(string(raw)), expfmt.FmtText)
	var foundStatus, foundLatency bool
	var online bool
	var latency int64
	var stable string
	for count := 0; count < 8; count++ {
		var family metricdto.MetricFamily
		if err := decoder.Decode(&family); err == io.EOF {
			if foundStatus && foundLatency && stable != "" {
				return online, latency, nil
			}
			break
		} else if err != nil {
			return false, 0, errors.New("TOOL_RESULT_INVALID")
		}
		if family.GetName() != "xray_proxy_status" && family.GetName() != "xray_proxy_latency_ms" {
			continue
		}
		if len(family.Metric) != 1 {
			return false, 0, errors.New("TOOL_RESULT_SCOPE")
		}
		m := family.Metric[0]
		var labelName, id string
		for _, label := range m.Label {
			if label.GetName() == "name" {
				labelName = label.GetValue()
			}
			if label.GetName() == "stable_id" {
				id = label.GetValue()
			}
		}
		if labelName != name || id == "" || stable != "" && id != stable || m.Gauge == nil {
			return false, 0, errors.New("TOOL_RESULT_SCOPE")
		}
		stable = id
		value := m.Gauge.GetValue()
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false, 0, errors.New("TOOL_RESULT_INVALID")
		}
		if family.GetName() == "xray_proxy_status" {
			if foundStatus || value != 0 && value != 1 {
				return false, 0, errors.New("TOOL_RESULT_INVALID")
			}
			foundStatus = true
			online = value == 1
		} else {
			if foundLatency || value < 0 || value > 60000 {
				return false, 0, errors.New("TOOL_RESULT_INVALID")
			}
			foundLatency = true
			latency = int64(value)
		}
	}
	return false, 0, errors.New("TOOL_RESULT_INVALID")
}

func (r Runner) knife(ctx context.Context, tool Tool, input Input, dir, target, engine string, result dto.Observation) dto.Observation {
	if os.WriteFile(filepath.Join(dir, "input.txt"), []byte(input.Link+"\n"), 0o600) != nil {
		return result
	}
	out := filepath.Join(dir, "result.jsonl")
	args := []string{"--db", filepath.Join(dir, "job.db"), "http", "--file", filepath.Join(dir, "input.txt"), "--out", out, "--type", "jsonl", "--core", strings.TrimPrefix(engine, "knife-"), "--threads", "1", "--timeout", "20000", "--mdelay", "20000", "--retries", "0", "--url", target, "--rip=false", "--diagnose=false", "--speedtest=false", "--prescan=false", "--ping=false", "--insecure=false"}
	executionErr := run(ctx, tool, dir, args)
	if executionErr != nil && (executionErr.Error() != "TOOL_EXECUTION_FAILED" || ctx.Err() != nil) {
		result.Code = executionErr.Error()
		return result
	}
	f, err := os.Open(out)
	if err != nil {
		result.Code = "TOOL_RESULT_MISSING"
		return result
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		result.Code = "TOOL_RESULT_LIMIT"
		return result
	}
	parsed, err := ParseKnife(raw, input.Link)
	if err != nil {
		result.Code = err.Error()
		return result
	}
	if executionErr != nil && parsed.Outcome == "passed" {
		result.Code = "TOOL_EXECUTION_FAILED"
		return result
	}
	result.Outcome = parsed.Outcome
	result.Code = parsed.Code
	result.StatusCode = parsed.StatusCode
	result.TTFBMS = parsed.TTFBMS
	result.FailureClass = parsed.FailureClass
	return result
}

func ParseKnife(raw []byte, link string) (dto.Observation, error) {
	var value struct {
		Link   string `json:"link"`
		Status string `json:"status"`
		Reason string `json:"reason"`
		Code   int    `json:"code"`
		TTFB   int64  `json:"ttfb"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if decoder.Decode(&value) != nil {
		return dto.Observation{}, errors.New("TOOL_RESULT_INVALID")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || value.Link != link || value.TTFB < 0 || value.TTFB > 60000 {
		return dto.Observation{}, errors.New("TOOL_RESULT_SCOPE")
	}
	result := dto.Observation{Outcome: "failed", FailureClass: "traffic", Code: "HTTPS_CHECK_FAILED", StatusCode: value.Code, TTFBMS: value.TTFB}
	if value.Status != "passed" && value.Status != "failed" && value.Status != "semi-passed" && value.Status != "broken" && value.Status != "timeout" && value.Status != "canceled" {
		return dto.Observation{}, errors.New("TOOL_RESULT_INVALID")
	}
	if result.StatusCode < 100 || result.StatusCode > 599 {
		result.StatusCode = 0
	}
	if value.Status == "passed" && value.Code == 204 {
		result.Outcome = "passed"
		result.Code = ""
		result.FailureClass = ""
	}
	if value.Status == "broken" || value.Status == "canceled" {
		result.Outcome = "observer_error"
		result.FailureClass = "observer"
		result.Code = "TOOL_PROFILE_UNAVAILABLE"
		if value.Status == "broken" && strings.Contains(value.Reason, "vless without TLS or other encryption is prohibited unless the server address is a private IP or domain") {
			result.Code = "TOOL_REQUIRES_ENCRYPTION"
		}
	}
	return result, nil
}

// A client-only generated profile has an opaque label before it enters a tool.
func OpaqueLink(link, key string) (string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", err
	}
	u.Fragment = key
	return u.String(), nil
}
