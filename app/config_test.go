package app

import (
	"testing"

	"github.com/go-ini/ini"
)

const (
	mandatoryConfig = `[database]
	dialect=sqlite5
	connection_url=admin:zxc90zxc@/nc_url_shortener?charset=utf8&parseTime=True
	
	[http]
	listener=:8080
	read_timeout=11
	write_timeout=11
	`
	fullConfig = mandatoryConfig + `[log]
	filename=/var/log/golang-url-shortener.log
	max_file_size_in_mb=21
	max_backups=2
	max_age_in_days=29
	compress_backups=true
	`
)

var fullLoadFunc = func(location string) (*ini.File, error) {
	return ini.InsensitiveLoad([]byte(fullConfig))
}

var mandatoryLoadFunc = func(location string) (*ini.File, error) {
	return ini.InsensitiveLoad([]byte(mandatoryConfig))
}

func TestGetConfiguration_Mandatory(t *testing.T) {
	SetupNewConfiguration(mandatoryLoadFunc)
	config, err := GetConfiguration("hello")
	if err != nil {
		t.Error("Unexpected error parsing mandatory configs! ", err)
	}
	if config.GetHTTPReadTimeout() != defaultReadTimeoutInSeconds+1 {
		t.Error("Did not set correct HTTP read timeout")
	}
	if config.GetDBDialect() != "sqlite5" {
		t.Error("Did not set correct DB Dialect")
	}
	if config.IsLoggerConfigAvailable() {
		t.Error("Unexpected logger configuration")
	}
}

func TestGetConfiguration_FullConfig(t *testing.T) {
	SetupNewConfiguration(fullLoadFunc)
	config, err := GetConfiguration("hello")
	if err != nil {
		t.Error("Unexpected error parsing mandatory configs! ", err)
	}
	if !config.IsLoggerConfigAvailable() {
		t.Error("Unexpected logger configuration")
	}
	if config.GetMaxAgeForALogFile() != defaultMaxAgeOfLogFile+1 {
		t.Error("Incorrect max age!")
	}
	if config.GetMaxLogBackups() != defaultMaxBackups+1 {
		t.Error("Incorrect max log backup setting")
	}
	if config.GetMaxLogFileSize() != defaultMaxLogFileSize+1 {
		t.Error("Incorrect max log file size")
	}
}

func TestResetDefaultNewConfiguration(t *testing.T) {
	SetupNewConfiguration(fullLoadFunc)
	_, err := GetConfiguration("hello")
	if err != nil {
		t.Error("Unexpected error parsing mandatory configs! ", err)
	}
	ResetDefaultNewConfiguration()
	_, err = GetConfiguration("hello")
	if err == nil {
		t.Error("Should have errored reading config")
	}
}
