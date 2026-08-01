package api

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestFindOrCreateUserPersistsNewLogtoUserDisabled(t *testing.T) {
	originalDB := db.DB
	t.Cleanup(func() {
		db.DB = originalDB
	})

	testDB, err := gorm.Open(sqlite.Open("file:logto-user-approval?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := testDB.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate user table: %v", err)
	}
	db.DB = testDB

	api := &DesktopAuthAPI{}
	info := &auth.LogtoUserInfo{
		Sub:      "logto-subject",
		Username: "new-logto-user",
		Name:     "New Logto User",
	}

	created, disabled, err := api.findOrCreateUser(context.Background(), info)
	if err != nil {
		t.Fatalf("create Logto user: %v", err)
	}
	if !disabled {
		t.Fatal("new Logto user must require administrator approval")
	}
	if created.Enabled {
		t.Fatal("new Logto user returned enabled")
	}

	var stored model.User
	if err := testDB.First(&stored, created.ID).Error; err != nil {
		t.Fatalf("reload Logto user: %v", err)
	}
	if stored.Enabled {
		t.Fatal("new Logto user was persisted enabled")
	}
	if stored.Source != model.UserSourceLogto {
		t.Fatalf("unexpected source: %q", stored.Source)
	}

	found, disabled, err := api.findOrCreateUser(context.Background(), info)
	if err != nil {
		t.Fatalf("find existing Logto user: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("created duplicate user: got id %d, want %d", found.ID, created.ID)
	}
	if !disabled || found.Enabled {
		t.Fatal("unapproved Logto user must remain disabled on the next login")
	}
}
