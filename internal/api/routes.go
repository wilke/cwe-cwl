// Package api provides the REST API for the CWL service.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/state"
	"github.com/BV-BRC/cwe-cwl/pkg/auth"
)

// Server is the HTTP server for the CWL API.
type Server struct {
	config    *config.Config
	store     *state.Store
	validator *auth.TokenValidator
	router    chi.Router
	handler   *Handler
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, store *state.Store) *Server {
	validator := auth.NewTokenValidator(cfg.Auth.UserServiceURL, cfg.Auth.WorkspaceURL)

	s := &Server{
		config:    cfg,
		store:     store,
		validator: validator,
	}

	s.handler = NewHandler(cfg, store, validator)
	s.router = s.setupRoutes()

	return s
}

// setupRoutes configures the router with all API routes.
func (s *Server) setupRoutes() chi.Router {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(s.config.Server.WriteTimeout))

	// Health check (no auth required)
	r.Get("/health", s.handler.HealthCheck)

	// API routes (with auth)
	r.Route("/api/v1", func(r chi.Router) {
		// Apply auth middleware to all API routes
		if s.config.Auth.ValidateUserTokens {
			r.Use(s.AuthMiddleware)
		}

		// Workflow operations
		r.Route("/workflows", func(r chi.Router) {
			r.Post("/", s.handler.SubmitWorkflow)
			r.Get("/", s.handler.ListWorkflows)
			r.Get("/{id}", s.handler.GetWorkflow)
			r.Delete("/{id}", s.handler.CancelWorkflow)
			r.Post("/{id}/rerun", s.handler.RerunWorkflow)
			r.Get("/{id}/steps", s.handler.GetWorkflowSteps)
			r.Get("/{id}/outputs", s.handler.GetWorkflowOutputs)
		})

		// Validation endpoints
		r.Post("/validate", s.handler.ValidateCWL)
		r.Post("/validate-inputs", s.handler.ValidateInputs)

		// File operations
		r.Post("/upload", s.handler.UploadFile)
		r.Get("/files/{id}", s.handler.DownloadFile)

		// Admin routes
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.AdminMiddleware)

			r.Route("/workflows", func(r chi.Router) {
				r.Get("/", s.handler.AdminListWorkflows)
				r.Get("/{id}", s.handler.AdminGetWorkflow)
				r.Delete("/{id}", s.handler.AdminCancelWorkflow)
				r.Post("/{id}/rerun", s.handler.AdminRerunWorkflow)
				r.Get("/{id}/steps", s.handler.AdminGetWorkflowSteps)
				r.Post("/{id}/steps/{step_id}/requeue", s.handler.AdminRequeueStep)
			})
		})
	})

	return r
}

// AdminMiddleware restricts access to admin users.
func (s *Server) AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is disabled, allow admin endpoints in dev mode.
		if !s.config.Auth.ValidateUserTokens {
			next.ServeHTTP(w, r)
			return
		}

		user := auth.GetUserFromContext(r.Context())
		if user == nil {
			http.Error(w, "missing authentication token", http.StatusUnauthorized)
			return
		}

		if !s.isAdminUser(user) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) isAdminUser(user *auth.UserInfo) bool {
	if user == nil {
		return false
	}
	for _, admin := range s.config.Auth.AdminUsers {
		if admin == "" {
			continue
		}
		if admin == user.UserID || admin == user.Username || admin == user.Email {
			return true
		}
	}
	return false
}

// AuthMiddleware validates the authentication token.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.ExtractToken(r)
		if token == "" {
			http.Error(w, "missing authentication token", http.StatusUnauthorized)
			return
		}

		user, err := s.validator.ValidateToken(r.Context(), token)
		if err != nil {
			http.Error(w, "invalid authentication token", http.StatusUnauthorized)
			return
		}

		// Add user to context
		ctx := auth.SetUserInContext(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Router returns the chi router for custom configuration.
func (s *Server) Router() chi.Router {
	return s.router
}
