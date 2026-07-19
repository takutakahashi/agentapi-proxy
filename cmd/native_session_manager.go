package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/sessionmanager"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

var NativeSessionManagerCmd = &cobra.Command{
	Use:   "native-session-manager",
	Short: "Run an External Session Manager using native host processes",
	RunE:  runNativeSessionManager,
}

var nativeSessionManagerOptions struct {
	listen, upstreamURL, connectionToken, upstreamAuthToken, publicURL, stateDir, binaryPath string
}

func init() {
	f := NativeSessionManagerCmd.Flags()
	f.StringVar(&nativeSessionManagerOptions.listen, "listen", ":8080", "HTTP listen address")
	f.StringVar(&nativeSessionManagerOptions.upstreamURL, "upstream-url", "", "parent agentapi-proxy URL")
	f.StringVar(&nativeSessionManagerOptions.connectionToken, "connection-token", "", "ESM connection/HMAC token")
	f.StringVar(&nativeSessionManagerOptions.upstreamAuthToken, "upstream-auth-token", "", "optional parent proxy authentication token")
	f.StringVar(&nativeSessionManagerOptions.publicURL, "public-url", "", "URL used by the parent proxy to route sessions")
	f.StringVar(&nativeSessionManagerOptions.stateDir, "state-dir", "./native-sessions", "native session state directory")
	f.StringVar(&nativeSessionManagerOptions.binaryPath, "binary", "", "agentapi-proxy binary used for provisioners")
}

func runNativeSessionManager(_ *cobra.Command, _ []string) error {
	o := nativeSessionManagerOptions
	if o.upstreamURL == "" || o.connectionToken == "" || o.publicURL == "" {
		return fmt.Errorf("--upstream-url, --connection-token and --public-url are required")
	}
	manager, err := services.NewNativeSessionManager(o.stateDir, o.upstreamURL, o.connectionToken, o.upstreamAuthToken, o.binaryPath)
	if err != nil {
		return err
	}

	e := echo.New()
	e.HideBanner = true
	provisionerController := controllers.NewProvisionerController(manager, nil, nil, nil)
	e.POST("/internal/session-provisioners/connect", provisionerController.Connect)
	e.GET("/internal/session-provisioners/:sessionId/provision-requests", provisionerController.GetProvisionRequest)
	e.POST("/internal/session-provisioners/:sessionId/provision-requests/:requestId/status", provisionerController.UpdateProvisionRequestStatus)

	handlers := sessionmanager.NewHandlers(manager, o.connectionToken)
	if err := handlers.RegisterRoutes(e); err != nil {
		return err
	}
	e.GET("/healthz", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "ok"}) })
	e.Any("/:sessionId/*", nativeSessionProxy(manager, []byte(o.connectionToken)))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	worker := sessionmanager.NewAllocatorWorkerWithUpstreamAuth(manager, o.upstreamURL, o.connectionToken, o.upstreamAuthToken, o.publicURL)
	go worker.Start(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer shutdownCancel()
		_ = e.Shutdown(shutdownCtx)
	}()
	log.Printf("[NATIVE_ESM] listening on %s, upstream=%s, public_url=%s", o.listen, o.upstreamURL, o.publicURL)
	if err := e.Start(o.listen); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func nativeSessionProxy(manager *services.NativeSessionManager, secret []byte) echo.HandlerFunc {
	return func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "read body")
		}
		c.Request().Body = io.NopCloser(bytes.NewReader(body))
		ts := c.Request().Header.Get(hmacutil.TimestampHeader)
		sig := c.Request().Header.Get("X-Hub-Signature-256")
		msg := hmacutil.BuildMessage(c.Request().Method, c.Request().URL.RequestURI(), ts, body)
		if hmacutil.ValidateTimestamp(ts) != nil || !hmacutil.Verify(secret, msg, sig) {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid signature")
		}
		session := manager.GetSession(c.Param("sessionId"))
		if session == nil {
			return echo.NewHTTPError(http.StatusNotFound, "session not found")
		}
		target, _ := url.Parse("http://" + session.Addr())
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.FlushInterval = -1
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			suffix := strings.TrimPrefix(c.Request().URL.Path, "/"+c.Param("sessionId"))
			if suffix == "" {
				suffix = "/"
			}
			req.URL.Path = suffix
			req.URL.RawPath = ""
			req.Host = target.Host
		}
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
			log.Printf("[NATIVE_ESM] proxy error: %v", proxyErr)
			http.Error(w, "native session unavailable", http.StatusBadGateway)
		}
		proxy.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}
