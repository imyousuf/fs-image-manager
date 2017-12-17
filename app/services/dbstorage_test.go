package services

import (
	"testing"
)

type MockDBConfig struct {
	dbDialect          string
	dbConnectionString string
}

func (config *MockDBConfig) GetDBDialect() string {
	return config.dbDialect
}
func (config *MockDBConfig) GetDBConnectionURL() string {
	return config.dbConnectionString
}

func getMockDBConfig() *MockDBConfig {
	config := &MockDBConfig{dbDialect: "sqlite3", dbConnectionString: "test-img-mngr.sqlite"}
	return config
}

func cleanDB() {
	GetDB().DropTable(&DownloadedFileModel{}, &DownloadHistoryModel{}, &DeviceModel{})
	ReInitDBConnection()
}

func TestInitAndCheckDBConnection(t *testing.T) {
	if !InitAndCheckDBConnection(getMockDBConfig()) {
		t.Error("DB Connection init failed unexpectedly!")
	}
	dbConn := GetDB()
	dbConn.DropTable(&DeviceModel{})
	if InitAndCheckDBConnection(getMockDBConfig()) {
		t.Error("DB Connection init passed unexpectedly!")
	}
	ReInitDBConnection()
	wrongConfig := getMockDBConfig()
	wrongConfig.dbConnectionString = "/a/" + wrongConfig.dbConnectionString
	if InitAndCheckDBConnection(wrongConfig) {
		t.Error("Init passed unexpectedly for wrong DB Connxn String!")
	}
	ReInitDBConnection()
	if !InitAndCheckDBConnection(getMockDBConfig()) {
		t.Error("Subsequent successful DB connection could not be established")
	}
	cleanDB()
}
