package services

import (
	"log"
	"sync"

	"github.com/imyousuf/fs-image-manager/app"
	"github.com/jinzhu/gorm"
	// GORM MySQL driver
	_ "github.com/jinzhu/gorm/dialects/mysql"
	// GORM SQLite driver
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

const (
	connectionAttemptMaxTries = 5
)

var (
	db            *gorm.DB
	dbInitializer sync.Once
	successful    = false
)

// openDBConnection opens the DB connection pool and should called from application
func openDBConnection(config app.DBConfig) bool {
	if !successful {
		var err error
		dbInitializer.Do(func() {
			db, err = gorm.Open(config.GetDBDialect(), config.GetDBConnectionURL())
			if err == nil {
				successful = true
				db.AutoMigrate(&DownloadedFile{}, &DownloadHistory{}, &DeviceModel{})
			} else {
				log.Println(err)
			}
		})
	}
	return successful
}

// IsDBConnectionAvailable checks if DB connection is available.
// Returns true if connection is available else false
func IsDBConnectionAvailable() bool {
	return successful
}

// GetDB retrieve the DB connection pool
func GetDB() *gorm.DB {
	if !IsDBConnectionAvailable() {
		return nil
	}
	return db
}

// CloseDB closes the current DB connection. Returns true if closed successfully.
func CloseDB() bool {
	if IsDBConnectionAvailable() {
		err := db.Close()
		return err == nil
	}
	return false
}

// ReInitDBConnection allows the DB connection to be re-initialized
func ReInitDBConnection() {
	CloseDB()
	dbInitializer = sync.Once{}
	successful = false
}

// InitAndCheckDBConnection initializes and checks whether the DB Connection is
// good for the app to proceed
func InitAndCheckDBConnection(config app.DBConfig) bool {
	openDBConnection(config)
	dbConn := GetDB()
	if dbConn == nil {
		return false
	}
	hasTable := dbConn.HasTable(&DeviceModel{})
	if hasTable {
		return true
	}
	return false
}
