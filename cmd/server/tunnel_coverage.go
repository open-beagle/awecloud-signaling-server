package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

func expectedTunnelResourceIDs(raw string) ([]string, error) {
	cfg, err := service.ParseTunnelPortsConfig(raw)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, binding := range cfg.Ports {
		id := binding.ResourceID
		if strings.TrimSpace(id) == "" || seen[id] || id == "k8s-api" {
			return nil, fmt.Errorf("ports_config resource_id 为空、重复或占用保留值: %q", id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if cfg.K8sAPIEnabled {
		ids = append(ids, "k8s-api")
	}
	sort.Strings(ids)
	return ids, nil
}
