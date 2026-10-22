package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupJSONAndStdLogBridge(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	setup(&buf, "warn", "json")

	slog.Info("hidden")
	log.Printf("std log goes through slog")
	slog.Warn("visible", "k", "v")

	out := strings.TrimSpace(buf.String())
	if strings.Contains(out, "hidden") {
		t.Errorf("info record leaked at warn level: %s", out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("not JSON: %v: %s", err, out)
	}
	if rec["msg"] != "visible" || rec["k"] != "v" || rec["level"] != "WARN" {
		t.Errorf("unexpected record: %v", rec)
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, " WARN ": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, "": slog.LevelInfo, "bogus": slog.LevelInfo,
	} {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFromContextCarriesRequestFields(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	setup(&buf, "info", "json")

	ri := &RequestInfo{ID: "rid-1"}
	ctx := WithRequest(context.Background(), ri)
	ri.SetActor("t1", "u1") // set after the context was built, as auth does
	FromContext(ctx).Info("hello")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["request_id"] != "rid-1" || rec["tenant_id"] != "t1" || rec["user_id"] != "u1" {
		t.Errorf("missing request fields: %v", rec)
	}
}

func TestRecoverLogsPanicAndSurvives(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	setup(&buf, "info", "json")

	ctx := WithRequest(context.Background(), &RequestInfo{ID: "rid-panic"})
	called := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Recover(ctx, "test goroutine", func() { called = true })
		var m map[string]int
		m["boom"] = 1 // nil map write panics
	}()
	<-done

	if !called {
		t.Error("onPanic callback was not called")
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("no JSON log line: %v: %s", err, buf.String())
	}
	if rec["level"] != "ERROR" || rec["request_id"] != "rid-panic" || rec["goroutine"] != "test goroutine" {
		t.Errorf("unexpected record: %v", rec)
	}
	if st, _ := rec["stack"].(string); !strings.Contains(st, "TestRecoverLogsPanicAndSurvives") {
		t.Errorf("stack trace missing the panicking function: %q", st)
	}
}

func TestRecoverIsSilentWithoutPanic(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	setup(&buf, "info", "json")
	func() {
		defer Recover(context.Background(), "quiet")
	}()
	if buf.Len() != 0 {
		t.Errorf("unexpected log output: %s", buf.String())
	}
}

func TestWarnIf(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	setup(&buf, "info", "json")

	if WarnIf(context.Background(), "decode x", nil) || buf.Len() != 0 {
		t.Fatal("nil error must log nothing and return false")
	}
	if !WarnIf(context.Background(), "decode x", json.Unmarshal([]byte("{"), &struct{}{}), "id", "42") {
		t.Fatal("non-nil error must return true")
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["level"] != "WARN" || rec["id"] != "42" || rec["err"] == nil {
		t.Errorf("unexpected record: %v", rec)
	}
}
