package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdminEnabledMigrationPreservesExistingAccounts(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`CREATE TABLE admin (
		id integer PRIMARY KEY AUTOINCREMENT,
		username text NOT NULL,
		password_hash text NOT NULL,
		role text NOT NULL,
		created_at datetime,
		updated_at datetime
	)`).Error)
	require.NoError(t, database.Exec(`INSERT INTO admin (username, password_hash, role) VALUES (?, ?, ?)`, "existing-admin", "hash", "admin").Error)

	require.NoError(t, database.AutoMigrate(&Admin{}))

	var account Admin
	require.NoError(t, database.First(&account, "username = ?", "existing-admin").Error)
	require.True(t, account.Enabled)
}
