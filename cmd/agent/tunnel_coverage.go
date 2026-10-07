package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
)

func decodeExpectedResourceIDs(encoded string) ([]string, error) {
	if encoded == "" {
		return nil, fmt.Errorf("缺少 Server resource_id 基线，禁止仅按数量通过")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("resource_id 基线 Base64 无效: %w", err)
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("resource_id 基线 JSON 无效: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("resource_id 基线为空")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seen[id] {
			return nil, fmt.Errorf("resource_id 基线包含空值或重复值: %q", id)
		}
		seen[id] = true
	}
	return ids, nil
}

// Compare slices before indexing: a map would hide duplicate resource IDs.
func checkResourceCoverage(expected []string, statuses []*agent.TunnelResourceStatus) error {
	want, seen := map[string]bool{}, map[string]bool{}
	for _, id := range expected {
		want[id] = true
	}
	var missing, extra, duplicate, inactive []string
	for _, s := range statuses {
		if s == nil || strings.TrimSpace(s.ResourceID) == "" {
			return fmt.Errorf("statusz 包含 null 或空 resource_id")
		}
		id := s.ResourceID
		if seen[id] {
			duplicate = append(duplicate, id)
		}
		seen[id] = true
		if !want[id] {
			extra = append(extra, id)
		}
		if s.LocalPort <= 0 || s.LocalPort > 65535 {
			inactive = append(inactive, id)
		}
	}
	for id := range want {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	for _, ids := range [][]string{missing, extra, duplicate, inactive} {
		sort.Strings(ids)
	}
	if len(missing)+len(extra)+len(duplicate)+len(inactive) > 0 || len(statuses) == 0 {
		return fmt.Errorf("resource_id 不一致: missing=%q extra=%q duplicate=%q invalid_local_port=%q", missing, extra, duplicate, inactive)
	}
	return nil
}
