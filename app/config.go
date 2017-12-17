package app

import (
	"errors"
	"os"
	"sync"

	"github.com/go-ini/ini"
)

const (
	defaultReadTimeoutInSeconds               = 10
	defaultWriteTimeoutInSeconds              = 10
	defaultMaxLogFileSize                     = 20 //MB
	defaultMaxBackups                         = 1
	defaultMaxAgeOfLogFile                    = 28 //days
	compressEnabledForLogFileBackupsByDefault = true
	// DefaultConfigFilePath represents the default file location in CWD
	DefaultConfigFilePath = "image-manager.cfg"
)

var (
	// EmptyConfigurationForError Represents the configuration instance to be
	// used when there is a configuration error during load
	EmptyConfigurationForError = &Config{}
)

// LogConfig represents the interface for log related configuration
type LogConfig interface {
	IsLoggerConfigAvailable() bool
	GetLogFilename() string
	GetMaxLogFileSize() int
	GetMaxLogBackups() int
	GetMaxAgeForALogFile() int
	IsCompressionEnabledOnLogBackups() bool
}

// HTTPConfig represents the HTTP configuration related behaviors
type HTTPConfig interface {
	GetHTTPListeningAddr() string
	GetHTTPReadTimeout() uint
	GetHTTPWriteTimeout() uint
	GetStaticFileDir() string
}

// DBConfig represents DB configuration related behaviors
type DBConfig interface {
	GetDBDialect() string
	GetDBConnectionURL() string
}

// LibraryConfig represents the API for library related configuration
type LibraryConfig interface {
	GetLibraryRoot() string
	IsPNGSupported() bool
}

// Config represents the configuration for the application
type Config struct {
	dbDialect              string
	dbConnectionURL        string
	httpListeningAddr      string
	httpReadTimeout        uint
	httpWriteTimeout       uint
	staticFileDir          string
	logFilename            string
	maxFileSize            int
	maxBackups             int
	maxAge                 int
	compressBackupsEnabled bool
	libraryRoot            string
	pngSupported           bool
}

// GetLibraryRoot retrieves the root directory of the library
func (config *Config) GetLibraryRoot() string {
	return config.libraryRoot
}

func (config *Config) IsPNGSupported() bool {
	return config.pngSupported
}

// GetDBDialect retrieves the dialect for the db
func (config *Config) GetDBDialect() string {
	return config.dbDialect
}

// GetDBConnectionURL retrieves the connection URL for the db
func (config *Config) GetDBConnectionURL() string {
	return config.dbConnectionURL
}

// GetHTTPListeningAddr retrieves the connection string to listen to
func (config *Config) GetHTTPListeningAddr() string {
	return config.httpListeningAddr
}

// GetHTTPReadTimeout retrieves the connection read timeout
func (config *Config) GetHTTPReadTimeout() uint {
	return config.httpReadTimeout
}

// GetHTTPWriteTimeout retrieves the connection write timeout
func (config *Config) GetHTTPWriteTimeout() uint {
	return config.httpWriteTimeout
}

func (config *Config) GetStaticFileDir() string {
	return config.staticFileDir
}

// IsLoggerConfigAvailable checks is logger configuration is set since its optional
func (config *Config) IsLoggerConfigAvailable() bool {
	return len(config.logFilename) > 0
}

// GetLogFilename retrieves the file name of the log
func (config *Config) GetLogFilename() string {
	return config.logFilename
}

// GetMaxLogFileSize retrieves the max log file size before its rotated in MB
func (config *Config) GetMaxLogFileSize() int {
	return config.maxFileSize
}

// GetMaxLogBackups retrieves max rotated logs to retain
func (config *Config) GetMaxLogBackups() int {
	return config.maxBackups
}

// GetMaxAgeForALogFile retrieves maximum day to retain a rotated log file
func (config *Config) GetMaxAgeForALogFile() int {
	return config.maxAge
}

// IsCompressionEnabledOnLogBackups checks if log backups are compressed
func (config *Config) IsCompressionEnabledOnLogBackups() bool {
	return config.compressBackupsEnabled
}

var (
	defaultLoadFunc = func(configFilePath string) (*ini.File, error) {
		filePath := DefaultConfigFilePath
		if len(configFilePath) > 0 {
			if _, err := os.Stat(configFilePath); err == nil {
				filePath = configFilePath
			}
		}
		return ini.InsensitiveLoad(filePath)
	}
	loadConfiguration   = defaultLoadFunc
	locationInitializer sync.Once
)

// GetConfiguration gets the current state of application configuration
func GetConfiguration(configFilePath string) (*Config, error) {
	configuration := &Config{}
	cfg, err := loadConfiguration(configFilePath)
	if err != nil {
		return EmptyConfigurationForError, err
	}
	storageConfError := setupStorageConfiguration(cfg, configuration)
	if storageConfError != nil {
		return EmptyConfigurationForError, storageConfError
	}
	httpConfSetupError := setupHTTPConfiguration(cfg, configuration)
	if httpConfSetupError != nil {
		return EmptyConfigurationForError, httpConfSetupError
	}
	logConfSetupErr := setupLogConfiguration(cfg, configuration)
	if logConfSetupErr != nil {
		return EmptyConfigurationForError, logConfSetupErr
	}
	libConfSetupErr := setupLibraryConfiguration(cfg, configuration)
	if libConfSetupErr != nil {
		return EmptyConfigurationForError, libConfSetupErr
	}
	return configuration, nil
}

func setupLibraryConfiguration(cfg *ini.File, configuration *Config) error {
	libSection, secErr := cfg.GetSection("library")
	if secErr != nil {
		return secErr
	}
	libraryRoot, libRootErr := libSection.GetKey("library_root")
	if libRootErr != nil {
		return libRootErr
	}
	if fi, err := os.Stat(libraryRoot.String()); os.IsNotExist(err) {
		return err
	} else if !fi.IsDir() {
		return errors.New("Library Root ('library_root') must be a directory. " + libraryRoot.String())
	}
	configuration.libraryRoot = libraryRoot.String()
	pngSupport, pngSupportConfErr := libSection.GetKey("support_png")
	if pngSupportConfErr != nil {
		return pngSupportConfErr
	}
	configuration.pngSupported = pngSupport.MustBool(false)
	return nil
}

func setupStorageConfiguration(cfg *ini.File, configuration *Config) error {
	dbSection, secErr := cfg.GetSection("database")
	if secErr != nil {
		return secErr
	}
	dbDialect, dialectErr := dbSection.GetKey("dialect")
	if dialectErr != nil {
		return dialectErr
	}
	dbConnxn, connxnErr := dbSection.GetKey("connection_url")
	if connxnErr != nil {
		return connxnErr
	}
	configuration.dbDialect = dbDialect.String()
	configuration.dbConnectionURL = dbConnxn.String()
	return nil
}

func setupHTTPConfiguration(cfg *ini.File, configuration *Config) error {
	httpSection, httpSecErr := cfg.GetSection("http")
	if httpSecErr != nil {
		return httpSecErr
	}
	// listener=:8080
	httpListener, listenerErr := httpSection.GetKey("listener")
	if listenerErr != nil {
		return listenerErr
	}
	var timeoutFormatErr error
	// read_timeout=10
	httpReadTimeout, readTimeoutErr := httpSection.GetKey("read_timeout")
	var readTimeout uint
	if readTimeoutErr != nil {
		readTimeout = defaultReadTimeoutInSeconds
	} else {
		readTimeout, timeoutFormatErr = httpReadTimeout.Uint()
		if timeoutFormatErr != nil {
			readTimeout = defaultReadTimeoutInSeconds
		}
	}
	// write_timeout=10
	httpWriteTimeout, writeTimeoutErr := httpSection.GetKey("write_timeout")
	var writeTimeout uint
	if writeTimeoutErr != nil {
		writeTimeout = defaultWriteTimeoutInSeconds
	} else {
		writeTimeout, timeoutFormatErr = httpWriteTimeout.Uint()
		if timeoutFormatErr != nil {
			writeTimeout = defaultReadTimeoutInSeconds
		}
	}
	// static_file_dir=web/img-mngr/
	httpStaticFileDir, staticFileDirErr := httpSection.GetKey("static_file_dir")
	if staticFileDirErr != nil {
		return staticFileDirErr
	}

	configuration.httpListeningAddr = httpListener.String()
	configuration.httpReadTimeout = readTimeout
	configuration.httpWriteTimeout = writeTimeout
	configuration.staticFileDir = httpStaticFileDir.String()
	return nil
}

func setupLogConfiguration(cfg *ini.File, configuration *Config) error {
	logSection, logSecErr := cfg.GetSection("log")
	consoleLogOnly := true
	var (
		logFilename                     string
		maxFileSize, maxBackups, maxAge int
		compressBackups                 bool
	)
	if logSecErr == nil {
		logFilenameKey, filenameErr := logSection.GetKey("filename")
		if filenameErr != nil {
			return filenameErr
		}
		if len(logFilenameKey.String()) <= 0 {
			return errors.New("'filename' must be specified in [log] section")
		}
		consoleLogOnly = false
		logFilename = logFilenameKey.String()
		// max_file_size_in_mb=20
		maxFileSizeKey, err := logSection.GetKey("max_file_size_in_mb")
		if err == nil {
			maxFileSize = maxFileSizeKey.MustInt(defaultMaxLogFileSize)
		}
		// max_backups=1
		maxBackupsKey, err := logSection.GetKey("max_backups")
		if err == nil {
			maxBackups = maxBackupsKey.MustInt(defaultMaxBackups)
		}
		// max_age_in_days=28
		maxAgeKey, err := logSection.GetKey("max_age_in_days")
		if err == nil {
			maxAge = maxAgeKey.MustInt(defaultMaxAgeOfLogFile)
		}
		// compress_backups=true
		compressEnabledKey, err := logSection.GetKey("compress_backups")
		if err == nil {
			compressBackups = compressEnabledKey.MustBool(compressEnabledForLogFileBackupsByDefault)
		}
	}
	if !consoleLogOnly {
		configuration.logFilename = logFilename
		configuration.maxFileSize = maxFileSize
		configuration.maxBackups = maxBackups
		configuration.maxAge = maxAge
		configuration.compressBackupsEnabled = compressBackups
	}
	return nil
}

// SetupNewConfiguration allows the application to load configuration in an alternate way
func SetupNewConfiguration(newLoadFunc func(string) (*ini.File, error)) {
	loadConfiguration = newLoadFunc
	locationInitializer = sync.Once{}
}

// ResetDefaultNewConfiguration resets the default way of loading configuration
func ResetDefaultNewConfiguration() {
	SetupNewConfiguration(defaultLoadFunc)
}
