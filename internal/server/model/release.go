package model

import "time"

type Component string

const (
	ComponentAgent    Component = "agent"
	ComponentEndpoint Component = "endpoint"
	ComponentDesktop  Component = "desktop"
)

func (c Component) Valid() bool {
	return c == ComponentAgent || c == ComponentEndpoint || c == ComponentDesktop
}

type ReleaseStatus string

const (
	ReleaseStatusDraft     ReleaseStatus = "draft"
	ReleaseStatusPublished ReleaseStatus = "published"
	ReleaseStatusRevoked   ReleaseStatus = "revoked"
)

// Release is a publishable component version. Platform-specific files belong
// to Artifact records so the release remains the single version source.
type Release struct {
	ID                  string        `gorm:"primaryKey;size:36" json:"id"`
	Component           Component     `gorm:"size:20;not null;uniqueIndex:uk_release_component_version_commit,priority:1;index" json:"component"`
	Version             string        `gorm:"size:64;not null;uniqueIndex:uk_release_component_version_commit,priority:2" json:"version"`
	CommitID            string        `gorm:"size:40;not null;default:'';uniqueIndex:uk_release_component_version_commit,priority:3" json:"commit_id"`
	Channel             string        `gorm:"size:32;not null;default:'stable';index" json:"channel"`
	Status              ReleaseStatus `gorm:"size:20;not null;default:'draft';index" json:"status"`
	ReleaseNotes        string        `gorm:"type:text" json:"release_notes"`
	MinSupportedVersion string        `gorm:"size:64" json:"min_supported_version"`
	PublishedAt         *time.Time    `json:"published_at"`
	CreatedBy           uint64        `json:"created_by"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

func (Release) TableName() string {
	return "release"
}

type ArtifactStatus string

const (
	ArtifactStatusAvailable ArtifactStatus = "available"
	ArtifactStatusRevoked   ArtifactStatus = "revoked"
)

// Artifact describes one release file for a concrete platform.
type Artifact struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	ReleaseID   string         `gorm:"size:36;not null;uniqueIndex:uk_artifact_release_platform,priority:1;index" json:"release_id"`
	OS          string         `gorm:"size:32;not null;uniqueIndex:uk_artifact_release_platform,priority:2" json:"os"`
	Arch        string         `gorm:"size:32;not null;uniqueIndex:uk_artifact_release_platform,priority:3" json:"arch"`
	Role        string         `gorm:"size:32;not null;default:'app';uniqueIndex:uk_artifact_release_platform,priority:4" json:"role"`
	PackageType string         `gorm:"size:32;not null;default:'binary'" json:"package_type"`
	Filename    string         `gorm:"size:255;not null" json:"filename"`
	DownloadURL string         `gorm:"type:text;not null" json:"download_url"`
	Size        int64          `gorm:"not null" json:"size"`
	SHA256      string         `gorm:"size:64;not null" json:"sha256"`
	Signature   string         `gorm:"type:text" json:"signature"`
	KeyID       string         `gorm:"size:100" json:"key_id"`
	Status      ArtifactStatus `gorm:"size:20;not null;default:'available';index" json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func (Artifact) TableName() string {
	return "artifact"
}
