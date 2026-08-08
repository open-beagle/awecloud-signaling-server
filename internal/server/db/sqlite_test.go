package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type sqliteConcurrencyFixture struct {
	ID    int `gorm:"primaryKey"`
	Value string
}

func (sqliteConcurrencyFixture) TableName() string { return "database_concurrency_fixture" }

func TestSQLiteDSNAllowsReadsDuringWriteTransaction(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(sqliteDSN(filepath.Join(t.TempDir(), "concurrency.db"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&sqliteConcurrencyFixture{}))
	require.NoError(t, database.Create(&sqliteConcurrencyFixture{ID: 1, Value: "before"}).Error)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConnections)
	sqlDB.SetMaxIdleConns(sqliteMaxOpenConnections)

	tx, err := sqlDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE database_concurrency_fixture SET value = 'during' WHERE id = 1`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var value string
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT value FROM database_concurrency_fixture WHERE id = 1`).Scan(&value))
	require.Equal(t, "before", value)
}

func TestSQLiteDSNWaitsForConcurrentWriter(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(sqliteDSN(filepath.Join(t.TempDir(), "writers.db"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&sqliteConcurrencyFixture{}))
	require.NoError(t, database.Create(&sqliteConcurrencyFixture{ID: 1, Value: "before"}).Error)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConnections)
	sqlDB.SetMaxIdleConns(sqliteMaxOpenConnections)

	tx, err := sqlDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.Exec(`UPDATE database_concurrency_fixture SET value = 'first' WHERE id = 1`)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, updateErr := sqlDB.Exec(`UPDATE database_concurrency_fixture SET value = 'second' WHERE id = 1`)
		result <- updateErr
	}()

	select {
	case updateErr := <-result:
		require.Failf(t, "writer returned before lock release", "error: %v", updateErr)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	require.NoError(t, <-result)
}

func TestSQLiteDSNPreventsDeferredTransactionSnapshotConflict(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(sqliteDSN(filepath.Join(t.TempDir(), "snapshot.db"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&sqliteConcurrencyFixture{}))
	require.NoError(t, database.Create(&sqliteConcurrencyFixture{ID: 1, Value: "before"}).Error)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConnections)
	sqlDB.SetMaxIdleConns(sqliteMaxOpenConnections)

	tx, err := sqlDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	var value string
	require.NoError(t, tx.QueryRow(`SELECT value FROM database_concurrency_fixture WHERE id = 1`).Scan(&value))
	require.Equal(t, "before", value)

	result := make(chan error, 1)
	go func() {
		_, updateErr := sqlDB.Exec(`UPDATE database_concurrency_fixture SET value = 'second' WHERE id = 1`)
		result <- updateErr
	}()
	time.Sleep(100 * time.Millisecond)
	_, err = tx.Exec(`UPDATE database_concurrency_fixture SET value = 'first' WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-result)
}
