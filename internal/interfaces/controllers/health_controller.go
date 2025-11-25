package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type HealthController struct{}

func NewHealthController() *HealthController {
	return &HealthController{}
}

func (c *HealthController) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/health", c.HealthCheck).Methods("GET")
}

type HealthResponse struct {
	Status string `json:"status"`
}

func (c *HealthController) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status: "ok",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
