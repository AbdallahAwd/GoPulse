package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoveryPreservesAbortedStreamingResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := RecoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial archive"))
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("stream abort was swallowed")
		}
		if recorder.Body.String() != "partial archive" {
			t.Error("HTTP error text appended to archive")
		}
	}()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/archive", nil))
}
