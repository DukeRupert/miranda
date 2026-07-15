package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// RecoveryConfig controls panic recovery behavior.
type RecoveryConfig struct {
	// LogFunc is called with the panic value and stack trace.
	// Defaults to slog.Error if nil.
	LogFunc func(val any, stack []byte)

	// ErrorHandler writes the full error response when a panic is recovered.
	// Defaults to a plain-text 500 "Internal Server Error" if nil.
	ErrorHandler func(w http.ResponseWriter, r *http.Request, val any)
}

// Recovery returns middleware that catches panics, logs them, and returns
// a 500 response instead of crashing the server process.
func Recovery(config RecoveryConfig) func(http.Handler) http.Handler {
	// 1. Resolve defaults for the log function and error handler.
	logFunc := config.LogFunc
	if logFunc == nil {
		logFunc = func(val any, stack []byte) {
			slog.Error("panic recovered",
				"error", val,
				"stack", string(stack),
			)
		}
	}

	errorHandler := config.ErrorHandler
	if errorHandler == nil {
		errorHandler = func(w http.ResponseWriter, r *http.Request, val any) {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 2. Wrap the writer to track whether headers have already been
			//    sent. If a handler wrote a partial response before panicking,
			//    we can't send a clean 500 — the bytes are already on the wire.
			rw := &recoverWriter{ResponseWriter: w}

			defer func() {
				val := recover()
				if val == nil {
					return
				}

				// 3. Re-panic for ErrAbortHandler — it signals intentional
				//    connection termination (e.g. cancelled requests, reverse
				//    proxy aborts). Swallowing it causes connection leaks.
				if val == http.ErrAbortHandler {
					panic(val)
				}

				// 4. Capture the stack trace from the panic site before doing
				//    anything else. debug.Stack() is valid here because deferred
				//    functions see the stack as it was at the moment of the panic.
				stack := debug.Stack()

				// 5. Log the panic value and stack trace.
				logFunc(val, stack)

				// 6. Do not write a response body on upgraded connections
				//    (WebSocket). The connection handshake has already happened
				//    and writing HTTP response headers at this point would
				//    corrupt the connection.
				if r.Header.Get("Connection") == "Upgrade" {
					return
				}

				// 7. Only write an error response if headers haven't been
				//    sent yet. If they have, the client gets a partial response
				//    but the server stays up.
				if rw.written {
					return
				}

				errorHandler(w, r, val)
			}()

			next.ServeHTTP(rw, r)
		})
	}
}

// recoverWriter tracks whether the response has started writing.
type recoverWriter struct {
	http.ResponseWriter
	written bool
}

func (w *recoverWriter) WriteHeader(code int) {
	w.written = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *recoverWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(b)
}
