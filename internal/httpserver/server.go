// =============================================================================
// internal/httpserver/server.go — HTTP API (Routes and Handlers)
// =============================================================================
//
// WHAT DOES THIS FILE DO?
//   It exposes the URL shortener over HTTP. It defines routes (e.g. POST /api/v1/shorten,
//   GET /{code}) and "handlers" — functions that run when a request hits that route.
//   Handlers read the request (e.g. JSON body), call the shortener service, and write
//   the response (JSON or redirect). It also adds "middleware" (request ID, timeout,
//   panic recovery) that runs for every request.
//
// GO CONCEPTS:
//   - net/http: the standard library's HTTP types. Handler is an interface with ServeHTTP(w, r).
//   - ResponseWriter: you write the response by calling w.WriteHeader(), w.Write(), or
//     json.NewEncoder(w).Encode(...). You don't return the response; you write it.
//   - Request: contains Method, URL, Headers, Body. request.Context() is the request's
//     context; it gets cancelled when the client disconnects.
//   - Chi router: chi.Mux lets us register routes (Get, Post) and middleware (Use).
//     It matches the request path and method and calls the right handler.
//
// REQUEST FLOW (example: user visits https://yoursite.com/abc123):
//   1. Request hits the server. Chi matches GET /{code} -> handleRedirect.
//   2. handleRedirect gets "abc123" from the URL, calls shortener.Resolve("abc123").
//   3. Resolve returns the long URL. We call TrackClick (async), then http.Redirect to the long URL.
//   4. Response is sent: HTTP 302 with Location: <long URL>.
//
// =============================================================================

package httpserver

import (
	"encoding/json" // JSON encoding/decoding (Encode, Decode)
	"net"           // net.SplitHostPort for parsing "host:port"
	"net/http"      // Handler, Request, ResponseWriter, status codes
	"strings"      // strings.Split, TrimSpace for headers
	"time"

	"urlshortener/internal/config"
	"urlshortener/internal/shortener"

	"github.com/go-chi/chi/v5"         // Router: Get, Post, Use, URLParam
	"github.com/go-chi/chi/v5/middleware" // RequestID, RealIP, Recoverer, Timeout
)

// Server holds everything the HTTP layer needs: config, logger, the shortener service,
// and the chi router. We build it once in main and pass it to http.Server as the Handler.
type Server struct {
	applicationConfig config.Config
	logger            Logger
	shortenerService  *shortener.Service
	router            *chi.Mux
}

// Logger is a small interface: only Printf. We use an interface so that in tests we can
// pass a fake logger. In Go, interfaces are satisfied implicitly: any type that has
// Printf(format string, args ...interface{}) implements Logger.
type Logger interface {
	Printf(format string, args ...interface{})
}

// NewServer builds the Server and registers all routes (setupRoutes). The router is
// created with chi.NewRouter(). We then pass this Server's router to http.Server in main.
func NewServer(applicationConfig config.Config, logger Logger, shortenerService *shortener.Service) *Server {
	server := &Server{
		applicationConfig: applicationConfig,
		logger:            logger,
		shortenerService:  shortenerService,
		router:            chi.NewRouter(),
	}
	server.setupRoutes()
	return server
}

// Router returns the chi router so main can pass it to http.Server{ Handler: ... }.
// The chi.Mux type has a ServeHTTP method, so it implements http.Handler.
func (server *Server) Router() http.Handler {
	return server.router
}

// setupRoutes registers middleware and route -> handler mappings.
//
// Middleware runs for every request before the handler. Order matters:
//   - RequestID: adds a unique ID to each request (for logging/tracing).
//   - RealIP: reads X-Forwarded-For or X-Real-IP so we get the client IP behind a proxy.
//   - Recoverer: catches panics and returns 500 instead of crashing the server.
//   - Timeout(10s): if a handler takes longer than 10s, the request is cancelled.
//
// Routes:
//   GET /healthz         -> health check (returns {"status":"ok"}).
//   POST /api/v1/shorten -> create short link (body: JSON with url, optional expiry).
//   GET /api/v1/stats/{code} -> get click stats for a code (JSON).
//   GET /{code}           -> redirect to original URL and track click.
//   GET /dashboard/{code} -> same as stats for now (placeholder for a future UI).
func (server *Server) setupRoutes() {
	router := server.router

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(10 * time.Second))

	router.Get("/healthz", server.handleHealth)
	router.Post("/api/v1/shorten", server.handleShorten)
	router.Get("/api/v1/stats/{code}", server.handleStats)

	router.Get("/{code}", server.handleRedirect)

	router.Get("/dashboard/{code}", server.handleDashboard)
}

// handleHealth is a simple liveness check. Kubernetes or load balancers can call
// GET /healthz to see if the server is up. We set Content-Type and write a small JSON body.
func (server *Server) handleHealth(responseWriter http.ResponseWriter, request *http.Request) {
	responseWriter.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(responseWriter).Encode(map[string]string{"status": "ok"})
}

// handleShorten handles POST /api/v1/shorten.
// We decode the request body (JSON) into shortener.ShortenRequest. Then we call
// Shorten on the service. If it succeeds, we write the response as JSON with status 201.
// If the JSON is invalid we return 400; if Shorten fails we return 500.
func (server *Server) handleShorten(responseWriter http.ResponseWriter, request *http.Request) {
	requestContext := request.Context()

	var shortenRequest shortener.ShortenRequest
	if err := json.NewDecoder(request.Body).Decode(&shortenRequest); err != nil {
		http.Error(responseWriter, "invalid JSON body", http.StatusBadRequest)
		return
	}

	baseURL := strings.TrimRight("http://"+request.Host, "/")

	shortenResponse, err := server.shortenerService.Shorten(requestContext, baseURL, shortenRequest)
	if err != nil {
		server.logger.Printf("shorten error: %v", err)
		http.Error(responseWriter, "failed to shorten URL", http.StatusInternalServerError)
		return
	}

	writeJSON(responseWriter, http.StatusCreated, shortenResponse)
}

// handleRedirect handles GET /{code}. This is the "click" flow: user visits our short URL,
// we look up the long URL, record the click (async), and redirect the browser.
// chi.URLParam(request, "code") extracts the "code" path parameter (e.g. "abc123" from /abc123).
// If the link is expired we return 410 Gone; if not found we return 404.
func (server *Server) handleRedirect(responseWriter http.ResponseWriter, request *http.Request) {
	requestContext := request.Context()
	shortCode := chi.URLParam(request, "code")

	originalURL, err := server.shortenerService.Resolve(requestContext, shortCode)
	if err != nil {
		if err == shortener.ErrExpired {
			http.Error(responseWriter, "link expired", http.StatusGone)
			return
		}
		http.NotFound(responseWriter, request)
		return
	}

	clientIP := clientIPFromRequest(request)
	userAgent := request.UserAgent()
	server.shortenerService.TrackClick(requestContext, shortCode, clientIP, userAgent)

	http.Redirect(responseWriter, request, originalURL, http.StatusTemporaryRedirect)
}

// handleStats returns click statistics for a short code (last 30 days). We call the
// service's Stats method and return the result as JSON. GET /api/v1/stats/abc123.
func (server *Server) handleStats(responseWriter http.ResponseWriter, request *http.Request) {
	requestContext := request.Context()
	shortCode := chi.URLParam(request, "code")

	statsSince := time.Now().Add(-30 * 24 * time.Hour)
	analyticsStats, err := server.shortenerService.Stats(requestContext, shortCode, statsSince)
	if err != nil {
		server.logger.Printf("stats error: %v", err)
		http.Error(responseWriter, "failed to load stats", http.StatusInternalServerError)
		return
	}

	writeJSON(responseWriter, http.StatusOK, analyticsStats)
}

// handleDashboard is a placeholder. For now it just returns the same JSON as handleStats.
// Later you could render an HTML page or embed a chart library here.
func (server *Server) handleDashboard(responseWriter http.ResponseWriter, request *http.Request) {
	server.handleStats(responseWriter, request)
}

// writeJSON sets Content-Type to application/json, writes the status code, and encodes
// responsePayload as JSON into the response body. We use it in every handler that returns JSON.
func writeJSON(responseWriter http.ResponseWriter, statusCode int, responsePayload any) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	_ = json.NewEncoder(responseWriter).Encode(responsePayload)
}

// clientIPFromRequest returns the client's IP. When the app is behind a load balancer or
// reverse proxy, the real IP is often in X-Forwarded-For (comma-separated list; we take the first).
// Otherwise we parse request.RemoteAddr (e.g. "192.168.1.1:54321") and return the IP part.
func clientIPFromRequest(request *http.Request) string {
	forwardedForHeader := request.Header.Get("X-Forwarded-For")
	if forwardedForHeader != "" {
		forwardedIPParts := strings.Split(forwardedForHeader, ",")
		return strings.TrimSpace(forwardedIPParts[0])
	}

	clientIP, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return clientIP
}
