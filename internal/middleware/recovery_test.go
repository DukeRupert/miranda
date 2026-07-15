package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRecovery_NoPanic_PassesThrough(t *testing.T) {
	handler := Recovery(RecoveryConfig{})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", rec.Body.String())
	}
}

func TestRecovery_PanicString_Returns500(t *testing.T) {
	handler := Recovery(RecoveryConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something broke")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestRecovery_PanicError_Returns500(t *testing.T) {
	handler := Recovery(RecoveryConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(struct{ msg string }{"unexpected"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestRecovery_CallsLogFunc(t *testing.T) {
	var mu sync.Mutex
	var loggedVal any
	var loggedStack []byte

	handler := Recovery(RecoveryConfig{
		LogFunc: func(val any, stack []byte) {
			mu.Lock()
			loggedVal = val
			loggedStack = stack
			mu.Unlock()
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	mu.Lock()
	defer mu.Unlock()

	if loggedVal != "test panic" {
		t.Fatalf("expected logged value %q, got %v", "test panic", loggedVal)
	}
	if len(loggedStack) == 0 {
		t.Fatal("expected non-empty stack trace")
	}
	if !strings.Contains(string(loggedStack), "recovery_test.go") {
		t.Fatalf("expected stack to reference test file, got:\n%s", loggedStack)
	}
}

func TestRecovery_CustomErrorHandler(t *testing.T) {
	handler := Recovery(RecoveryConfig{
		LogFunc: func(any, []byte) {},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, val any) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("custom error page"))
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if rec.Body.String() != "custom error page" {
		t.Fatalf("expected custom body, got %q", rec.Body.String())
	}
}

func TestRecovery_ErrAbortHandler_RePanics(t *testing.T) {
	handler := Recovery(RecoveryConfig{
		LogFunc: func(any, []byte) {},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		val := recover()
		if val != http.ErrAbortHandler {
			t.Fatalf("expected ErrAbortHandler re-panic, got %v", val)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	t.Fatal("expected panic to propagate, but handler returned normally")
}

func TestRecovery_UpgradedConnection_NoBody(t *testing.T) {
	var logged bool
	handler := Recovery(RecoveryConfig{
		LogFunc: func(any, []byte) { logged = true },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("ws panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !logged {
		t.Fatal("expected panic to be logged")
	}
	// Should not write any error response on upgraded connections.
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body on upgraded connection, got %q", rec.Body.String())
	}
}

func TestRecovery_HeadersAlreadySent_NoDoubleWrite(t *testing.T) {
	var logged bool
	handler := Recovery(RecoveryConfig{
		LogFunc: func(any, []byte) { logged = true },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
		panic("mid-response panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !logged {
		t.Fatal("expected panic to be logged")
	}
	// The original 200 + partial body should be preserved, not overwritten with 500.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected original status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("expected original body %q, got %q", "partial", rec.Body.String())
	}
}

func TestRecovery_ServerStaysUp_AfterPanic(t *testing.T) {
	handler := Recovery(RecoveryConfig{
		LogFunc: func(any, []byte) {},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			panic("boom")
		}
		w.Write([]byte("ok"))
	}))

	// First request panics.
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic request: expected 500, got %d", rec.Code)
	}

	// Second request should succeed — server didn't crash.
	req = httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("normal request after panic: expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", rec.Body.String())
	}
}
