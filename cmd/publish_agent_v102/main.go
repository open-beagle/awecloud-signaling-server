package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

func main() {
	dbPath := "data/server.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	binaryPath := "bin/signal_agent-linux-amd64"
	if _, err := os.Stat(binaryPath); err != nil {
		log.Fatalf("Binary not found: %s", binaryPath)
	}

	// Calculate SHA256 and size
	fileInfo, err := os.Stat(binaryPath)
	if err != nil {
		log.Fatalf("Stat binary failed: %v", err)
	}
	file, err := os.Open(binaryPath)
	if err != nil {
		log.Fatalf("Open binary failed: %v", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		file.Close()
		log.Fatalf("Copy binary failed: %v", err)
	}
	file.Close()
	digest := hex.EncodeToString(h.Sum(nil))

	// Keypair generation or loading
	pubKeyBase64 := os.Getenv("SIGNAL_UPDATER_PUBLIC_KEY")
	privKeyBase64 := os.Getenv("SIGNAL_UPDATER_PRIVATE_KEY")
	if pubKeyBase64 == "" {
		pubKeyBase64 = "FNTF+PH1fwBGXWeIiM01aMjhyPT/wBEruO0QKduunpw="
		privKeyBase64 = "BgD627p10DiVTqUScNJ9khk5mTpCnMmssmhRbX8OyswU1MX48fV/AEZdZ4iIzTVoyOHI9P/AESu47RAp266enA=="
	}

	var pubKey ed25519.PublicKey
	var privKey ed25519.PrivateKey

	if pubKeyBase64 != "" && privKeyBase64 != "" {
		pubBytes, _ := base64.StdEncoding.DecodeString(pubKeyBase64)
		privBytes, _ := base64.StdEncoding.DecodeString(privKeyBase64)
		pubKey = ed25519.PublicKey(pubBytes)
		privKey = ed25519.PrivateKey(privBytes)
	} else {
		var err error
		pubKey, privKey, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatalf("Generate ed25519 key failed: %v", err)
		}
		pubKeyBase64 = base64.StdEncoding.EncodeToString(pubKey)
		privKeyBase64 = base64.StdEncoding.EncodeToString(privKey)
	}

	sigBytes := ed25519.Sign(privKey, []byte(digest))
	sigBase64 := base64.StdEncoding.EncodeToString(sigBytes)

	fmt.Printf("SIGNAL_UPDATER_PUBLIC_KEY=%s\n", pubKeyBase64)
	fmt.Printf("SIGNAL_UPDATER_PRIVATE_KEY=%s\n", privKeyBase64)
	fmt.Printf("Artifact SHA256: %s\n", digest)
	fmt.Printf("Artifact Size: %d\n", fileInfo.Size())
	fmt.Printf("Artifact Signature: %s\n", sigBase64)

	// Connect DB
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("Connect SQLite DB failed: %v", err)
	}

	// Auto migrate new fields (commit_id, binary_sha256, artifact_id, etc.)
	if err := db.AutoMigrate(
		&model.Release{},
		&model.Artifact{},
		&model.UpdateTask{},
		&model.UpdateEvent{},
		&model.Node{},
	); err != nil {
		log.Fatalf("AutoMigrate failed: %v", err)
	}

	commitID := os.Getenv("GIT_COMMIT")
	if commitID == "" {
		commitID = "f77de3d30dc27481a72c113e22dd16f68471dedc"
	}

	version := "v1.0.2"

	// Upsert Release
	var release model.Release
	err = db.Where("component = ? AND version = ?", model.ComponentAgent, version).First(&release).Error
	if err != nil {
		release = model.Release{
			ID:           uuid.NewString(),
			Component:    model.ComponentAgent,
			Version:      version,
			CommitID:     commitID,
			Channel:      "stable",
			Status:       model.ReleaseStatusPublished,
			ReleaseNotes: "v1.0.2 Agent ZTNA Updater Release",
		}
		if err := db.Create(&release).Error; err != nil {
			log.Fatalf("Create release failed: %v", err)
		}
		fmt.Printf("Created Release %s (%s)\n", release.Version, release.ID)
	} else {
		release.CommitID = commitID
		release.Status = model.ReleaseStatusPublished
		db.Save(&release)
		fmt.Printf("Updated Release %s (%s)\n", release.Version, release.ID)
	}

	// Upsert Artifact
	var artifact model.Artifact
	err = db.Where("release_id = ? AND os = ? AND arch = ?", release.ID, "linux", "amd64").First(&artifact).Error
	if err != nil {
		artifact = model.Artifact{
			ID:          uuid.NewString(),
			ReleaseID:   release.ID,
			OS:          "linux",
			Arch:        "amd64",
			PackageType: "binary",
			Filename:    "signal_agent-linux-amd64",
			DownloadURL: fmt.Sprintf("https://signal.wodcloud.com/api/v1/download/agent?os=linux&arch=amd64&version=%s", version),
			Size:        fileInfo.Size(),
			SHA256:      digest,
			Signature:   sigBase64,
			KeyID:       "dev-key-1",
			Status:      model.ArtifactStatusAvailable,
		}
		if err := db.Create(&artifact).Error; err != nil {
			log.Fatalf("Create artifact failed: %v", err)
		}
		fmt.Printf("Created Artifact %s (%s)\n", artifact.Filename, artifact.ID)
	} else {
		artifact.Size = fileInfo.Size()
		artifact.SHA256 = digest
		artifact.Signature = sigBase64
		artifact.Status = model.ArtifactStatusAvailable
		db.Save(&artifact)
		fmt.Printf("Updated Artifact %s (%s)\n", artifact.Filename, artifact.ID)
	}

	// Find active agent node
	var nodes []model.Node
	db.Where("type = ?", model.NodeTypeAgent).Find(&nodes)
	fmt.Printf("Found %d Agent Nodes in DB:\n", len(nodes))
	for _, n := range nodes {
		fmt.Printf("  Node ID: %d, Name: %s, Version: %s, CommitID: %s, SHA256: %s\n",
			n.ID, n.Name, n.Version, n.CommitID, n.BinarySHA256)
	}

	if len(nodes) > 0 {
		updateSvc := service.NewUpdateService(db)
		task, err := updateSvc.CreateTask(nil, service.CreateUpdateTaskInput{
			Component:  model.ComponentAgent,
			TargetType: model.UpdateTargetNode,
			TargetID:   fmt.Sprintf("%d", nodes[0].ID),
			ReleaseID:  release.ID,
			Force:      true,
		})
		if err != nil {
			fmt.Printf("CreateUpdateTask note: %v\n", err)
		} else {
			fmt.Printf("Successfully created UpdateTask %s for Node %d!\n", task.ID, nodes[0].ID)
		}
	}
}
