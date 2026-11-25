package control

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type shutdownWriter struct {
	*httptest.ResponseRecorder
	flushed      bool
	disconnected bool
}

func (w *shutdownWriter) Write(data []byte) (int, error) {
	if w.disconnected {
		return 0, errors.New("client disconnected")
	}
	return w.ResponseRecorder.Write(data)
}
func (w *shutdownWriter) Flush() { w.flushed = true; w.ResponseRecorder.Flush() }

func TestShutdownAcknowledgesBeforeRequestAndAcceptsDuplicateOrDisconnectedClients(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		calls := 0
		writer := &shutdownWriter{ResponseRecorder: httptest.NewRecorder(), disconnected: disconnected}
		handler := HandlerWithShutdown(nil, func() {
			if !writer.flushed {
				t.Error("shutdown preceded acknowledgement flush")
			}
			calls++
		})
		for range 2 {
			handler.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/v1/shutdown", nil))
		}
		if calls != 2 || !writer.flushed {
			t.Fatalf("calls=%d flushed=%v", calls, writer.flushed)
		}
		if !disconnected && !strings.Contains(writer.Body.String(), `"stopping":true`) {
			t.Fatalf("response=%s", writer.Body.String())
		}
	}
}

func TestShutdownRejectsUnexpectedMethodBodyAndQuery(t *testing.T) {
	for _, test := range []struct {
		method, endpoint, body string
		status                 int
	}{
		{http.MethodGet, "/v1/shutdown", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/shutdown?now=true", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/shutdown?instance=", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/shutdown?instance=one&instance=two", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/shutdown", "{}", http.StatusBadRequest},
	} {
		handler := HandlerWithShutdown(nil, func() { t.Error("invalid request stopped service") })
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest(test.method, test.endpoint, strings.NewReader(test.body)))
		if writer.Code != test.status {
			t.Fatalf("%s %s=%d", test.method, test.endpoint, writer.Code)
		}
	}
}

type shutdownFixture struct{ testService }

func (*shutdownFixture) Status() Status { return Status{InstanceID: "current"} }

func TestShutdownRequiresMatchingInstanceWhenSpecified(t *testing.T) {
	stops := 0
	handler := HandlerWithShutdown(&shutdownFixture{}, func() { stops++ })
	oldInstance := httptest.NewRecorder()
	handler.ServeHTTP(oldInstance, httptest.NewRequest(http.MethodPost, "/v1/shutdown?instance=old", nil))
	if oldInstance.Code != http.StatusConflict || stops != 0 {
		t.Fatalf("stale shutdown responded %d with %d stops", oldInstance.Code, stops)
	}
	currentInstance := httptest.NewRecorder()
	handler.ServeHTTP(currentInstance, httptest.NewRequest(http.MethodPost, "/v1/shutdown?instance=current", nil))
	if currentInstance.Code != http.StatusOK || stops != 1 ||
		!strings.Contains(currentInstance.Body.String(), `"instance_id":"current"`) {
		t.Fatalf("matching shutdown responded %d (%s) with %d stops", currentInstance.Code, currentInstance.Body, stops)
	}
}
