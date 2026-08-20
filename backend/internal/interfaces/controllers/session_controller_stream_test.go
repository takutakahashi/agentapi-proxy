package controllers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestStreamTunnelResponseFlushesHeadersBeforeFirstBodyFrame(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/session/sse", nil), recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}

	if err := streamTunnelResponse(ctx, resp); err != nil {
		t.Fatal(err)
	}
	if !recorder.Flushed {
		t.Fatal("response headers were not flushed for an idle SSE stream")
	}
}
