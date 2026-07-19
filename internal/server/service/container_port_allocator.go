package service

import (
	"fmt"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"gorm.io/gorm"
)

const (
	ContainerSSHPortBase uint16 = 50200
	ContainerSSHPortEnd  uint16 = 51199
)

// EnsureContainerSSHPort assigns a stable port within the target Agent. The
// port is routing metadata only; Agent authorization remains resource-ID based.
func EnsureContainerSSHPort(tx *gorm.DB, resource *model.Resource, agentNodeID uint64) error {
	if agentNodeID == 0 {
		return fmt.Errorf("ContainerSSH Agent is required")
	}
	if resource.AgentNodeID == agentNodeID && resource.ContainerSSHPort != 0 {
		return nil
	}
	for port := ContainerSSHPortBase; port <= ContainerSSHPortEnd; port++ {
		var count int64
		if err := tx.Model(&model.Resource{}).
			Where("agent_node_id = ? AND container_ssh_port = ? AND id <> ?", agentNodeID, port, resource.ID).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			resource.AgentNodeID = agentNodeID
			resource.ContainerSSHPort = port
			return tx.Model(resource).Updates(map[string]interface{}{
				"agent_node_id": agentNodeID, "container_ssh_port": port,
			}).Error
		}
	}
	return fmt.Errorf("ContainerSSH port range exhausted for Agent %d", agentNodeID)
}
