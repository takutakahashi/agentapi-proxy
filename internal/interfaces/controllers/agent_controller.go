package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/agent"
)

type AgentController struct {
	agentManager *agent.AgentManager
}

func NewAgentController(agentManager *agent.AgentManager) *AgentController {
	return &AgentController{
		agentManager: agentManager,
	}
}

func (c *AgentController) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/sessions/{sessionId}/agents", c.CreateAgent).Methods("POST")
	router.HandleFunc("/sessions/{sessionId}/agents", c.ListAgents).Methods("GET")
	router.HandleFunc("/agents/{agentId}", c.GetAgent).Methods("GET")
	router.HandleFunc("/agents/{agentId}/start", c.StartAgent).Methods("POST")
	router.HandleFunc("/agents/{agentId}/stop", c.StopAgent).Methods("POST")
	router.HandleFunc("/agents/{agentId}/health", c.HealthCheck).Methods("GET")
	router.HandleFunc("/sessions/{sessionId}/scale", c.ScaleAgents).Methods("POST")
}

type CreateAgentRequest struct {
	Metadata map[string]string `json:"metadata,omitempty"`
}

type CreateAgentResponse struct {
	AgentID  string            `json:"agentId"`
	PodName  string            `json:"podName"`
	Status   string            `json:"status"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (c *AgentController) CreateAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := entities.SessionID(vars["sessionId"])

	agent, err := c.agentManager.CreateAgent(r.Context(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := CreateAgentResponse{
		AgentID:  string(agent.ID),
		PodName:  agent.PodName,
		Status:   string(agent.Status),
		Metadata: agent.Metadata,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type ListAgentsResponse struct {
	Agents []AgentInfo `json:"agents"`
}

type AgentInfo struct {
	AgentID    string            `json:"agentId"`
	PodName    string            `json:"podName"`
	Status     string            `json:"status"`
	CreatedAt  string            `json:"createdAt"`
	LastPingAt string            `json:"lastPingAt"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func (c *AgentController) ListAgents(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := entities.SessionID(vars["sessionId"])

	agents, err := c.agentManager.GetAgentsBySession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	agentInfos := make([]AgentInfo, 0, len(agents))
	for _, agent := range agents {
		agentInfos = append(agentInfos, AgentInfo{
			AgentID:    string(agent.ID),
			PodName:    agent.PodName,
			Status:     string(agent.Status),
			CreatedAt:  agent.CreatedAt.Format("2006-01-02T15:04:05Z"),
			LastPingAt: agent.LastPingAt.Format("2006-01-02T15:04:05Z"),
			Metadata:   agent.Metadata,
		})
	}

	response := ListAgentsResponse{
		Agents: agentInfos,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (c *AgentController) GetAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentID := entities.AgentID(vars["agentId"])

	agent, err := c.agentManager.GetAgent(r.Context(), agentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	response := AgentInfo{
		AgentID:    string(agent.ID),
		PodName:    agent.PodName,
		Status:     string(agent.Status),
		CreatedAt:  agent.CreatedAt.Format("2006-01-02T15:04:05Z"),
		LastPingAt: agent.LastPingAt.Format("2006-01-02T15:04:05Z"),
		Metadata:   agent.Metadata,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (c *AgentController) StartAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentID := entities.AgentID(vars["agentId"])

	if err := c.agentManager.StartAgent(r.Context(), agentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Agent started successfully",
	})
}

func (c *AgentController) StopAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentID := entities.AgentID(vars["agentId"])

	if err := c.agentManager.StopAgent(r.Context(), agentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Agent stopped successfully",
	})
}

func (c *AgentController) HealthCheck(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentID := entities.AgentID(vars["agentId"])

	if err := c.agentManager.HealthCheck(r.Context(), agentID); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

type ScaleAgentsRequest struct {
	TargetCount int `json:"targetCount"`
}

func (c *AgentController) ScaleAgents(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := entities.SessionID(vars["sessionId"])

	var req ScaleAgentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := c.agentManager.ScaleAgents(r.Context(), sessionID, req.TargetCount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "Scaling operation completed",
		"targetCount": req.TargetCount,
	})
}
