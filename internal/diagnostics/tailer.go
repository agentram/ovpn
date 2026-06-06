package diagnostics

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

const (
	defaultReadInterval      = 5 * time.Second
	defaultFlushInterval     = 30 * time.Second
	defaultCleanupInterval   = time.Hour
	defaultBatchSize         = 1000
	defaultMaxAccessLogBytes = 30 * 1024 * 1024
	offsetMetaKey            = "connection_access_log_offset"
	DefaultAccessLogPath     = "/var/log/ovpn/xray-access.log"
	DefaultDiagnosticsMode   = "basic"
	DiagnosticsModeOff       = "off"
	DiagnosticsModeBasic     = "basic"
	DiagnosticsModeDebug     = "debug"
)

type Observer interface {
	OnConnectionEvents(count int)
	OnConnectionParseError()
	OnConnectionTailerError(operation string)
}

type Tailer struct {
	Store             *remote.Store
	Path              string
	Mode              string
	MaxBytes          int64
	ReadInterval      time.Duration
	FlushInterval     time.Duration
	CleanupInterval   time.Duration
	BatchSize         int
	SourceBucketCap   int
	DebugEventCap     int
	Retention         time.Duration
	Logger            *slog.Logger
	Observer          Observer
	offset            int64
	partial           string
	offsetInitialized bool
}

func (t *Tailer) Run(ctx context.Context) error {
	if !DiagnosticsEnabled(t.Mode) {
		return nil
	}
	if t.Store == nil {
		return fmt.Errorf("diagnostics tailer store is nil")
	}
	if strings.TrimSpace(t.Path) == "" {
		t.Path = DefaultAccessLogPath
	}
	if t.MaxBytes <= 0 {
		t.MaxBytes = defaultMaxAccessLogBytes
	}
	if t.ReadInterval <= 0 {
		t.ReadInterval = defaultReadInterval
	}
	if t.FlushInterval <= 0 {
		t.FlushInterval = defaultFlushInterval
	}
	if t.CleanupInterval <= 0 {
		t.CleanupInterval = defaultCleanupInterval
	}
	if t.BatchSize <= 0 {
		t.BatchSize = defaultBatchSize
	}
	if t.Retention <= 0 {
		t.Retention = remote.DefaultConnectionRetention
	}
	log := t.logger().With("component", "connection-diagnostics", "path", t.Path, "mode", NormalizeMode(t.Mode))
	log.Info("connection diagnostics tailer started")
	readTicker := time.NewTicker(t.ReadInterval)
	flushTicker := time.NewTicker(t.FlushInterval)
	cleanupTicker := time.NewTicker(t.CleanupInterval)
	defer readTicker.Stop()
	defer flushTicker.Stop()
	defer cleanupTicker.Stop()

	batch := make([]model.ConnectionEvent, 0, t.BatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := t.Store.RecordConnectionEvents(ctx, batch, t.SourceBucketCap, t.DebugEventCap, time.Now().UTC()); err != nil {
			t.observeTailerError("record_events")
			log.Warn("record connection events failed", "events", len(batch), "error", err)
			return
		}
		t.observeEvents(len(batch))
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			log.Info("connection diagnostics tailer stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-readTicker.C:
			events, err := t.readNewEvents(ctx)
			if err != nil {
				t.observeTailerError("read_access_log")
				log.Debug("read access log skipped", "error", err)
				continue
			}
			for _, ev := range events {
				batch = append(batch, ev)
				if len(batch) >= t.BatchSize {
					flush()
				}
			}
		case <-flushTicker.C:
			flush()
		case <-cleanupTicker.C:
			if err := t.Store.CleanupConnectionDiagnostics(ctx, time.Now().UTC(), t.Retention); err != nil {
				t.observeTailerError("cleanup")
				log.Warn("connection diagnostics cleanup failed", "error", err)
			}
		}
	}
}

func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", DiagnosticsModeBasic:
		return DiagnosticsModeBasic
	case DiagnosticsModeOff:
		return DiagnosticsModeOff
	case DiagnosticsModeDebug:
		return DiagnosticsModeDebug
	default:
		return DiagnosticsModeBasic
	}
}

func DiagnosticsEnabled(mode string) bool {
	return NormalizeMode(mode) != DiagnosticsModeOff
}

func (t *Tailer) readNewEvents(ctx context.Context) ([]model.ConnectionEvent, error) {
	st, err := os.Stat(t.Path)
	if err != nil {
		return nil, err
	}
	if !t.offsetInitialized {
		if err := t.initOffset(ctx, st.Size()); err != nil {
			return nil, err
		}
	}
	size := st.Size()
	if size < t.offset {
		t.offset = 0
		t.partial = ""
	}
	if size == t.offset {
		if t.MaxBytes > 0 && size > t.MaxBytes {
			if err := t.truncateScratchLog(ctx); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	f, err := os.Open(t.Path) // #nosec G304 -- operator-controlled local path from deployment config.
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, t.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	chunk := string(raw)
	t.offset += int64(len(raw))
	if t.MaxBytes > 0 && size > t.MaxBytes {
		if err := t.truncateScratchLog(ctx); err != nil {
			return nil, err
		}
	}
	if err := t.Store.SetMeta(ctx, offsetMetaKey, strconv.FormatInt(t.offset, 10)); err != nil {
		return nil, err
	}
	if chunk == "" {
		return nil, nil
	}
	return t.parseChunk(chunk), nil
}

func (t *Tailer) truncateScratchLog(ctx context.Context) error {
	// The Xray access log is bounded scratch data, not authoritative state.
	// Truncation assumes Xray opens the file for append-style writes; bytes
	// written between our read window and this truncate may be dropped. That is
	// acceptable for best-effort diagnostics and keeps disk usage bounded.
	if err := os.Truncate(t.Path, 0); err != nil {
		return err
	}
	t.offset = 0
	t.partial = ""
	return t.Store.SetMeta(ctx, offsetMetaKey, "0")
}

func (t *Tailer) initOffset(ctx context.Context, size int64) error {
	if raw, ok, err := t.Store.GetMeta(ctx, offsetMetaKey); err != nil {
		return err
	} else if ok {
		if offset, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && offset >= 0 && offset <= size {
			t.offset = offset
		} else {
			t.offset = size
		}
	} else {
		t.offset = size
	}
	t.offsetInitialized = true
	return nil
}

func (t *Tailer) parseChunk(chunk string) []model.ConnectionEvent {
	text := t.partial + chunk
	lines := strings.Split(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		t.partial = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	} else {
		t.partial = ""
	}
	events := make([]model.ConnectionEvent, 0, len(lines))
	for _, line := range lines {
		ev, ok := ParseAccessLine(line)
		if !ok {
			if strings.TrimSpace(line) != "" {
				t.observeParseError()
			}
			continue
		}
		events = append(events, ev)
	}
	return events
}

func (t *Tailer) observeEvents(count int) {
	if t.Observer != nil && count > 0 {
		t.Observer.OnConnectionEvents(count)
	}
}

func (t *Tailer) observeParseError() {
	if t.Observer != nil {
		t.Observer.OnConnectionParseError()
	}
}

func (t *Tailer) observeTailerError(operation string) {
	if t.Observer != nil {
		t.Observer.OnConnectionTailerError(operation)
	}
}

func (t *Tailer) logger() *slog.Logger {
	if t != nil && t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}
