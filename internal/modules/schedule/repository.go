package schedule

import (
	"log"

	corerepo "github.com/takutakahashi/agentapi-proxy/internal/core/repository"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func extractRepositoryInfo(tags map[string]string, sessionID string) *entities.RepositoryInfo {
	repoInfo, err := corerepo.ExtractInfo(tags, sessionID)
	if err != nil {
		log.Printf("[SCHEDULE] Failed to extract repository info: %v", err)
		return nil
	}
	return repoInfo
}
