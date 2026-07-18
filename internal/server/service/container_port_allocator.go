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

// EnsureContainerSSHPort assigns a stable port once per Resource. The port is
// routing metadata only; Agent authorization remains resource-ID based.
func EnsureContainerSSHPort(tx *gorm.DB, resource *model.Resource) error {
	if resource.ContainerSSHPort != 0 {
		return nil
	}
	for port := ContainerSSHPortBase; port <= ContainerSSHPortEnd; port++ {
		var count int64
		if err := tx.Model(&model.Resource{}).Where("container_ssh_port = ?", port).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			resource.ContainerSSHPort = port
			return tx.Model(resource).Update("container_ssh_port", port).Error
		}
	}
	return fmt.Errorf("ContainerSSH port range exhausted")
}
