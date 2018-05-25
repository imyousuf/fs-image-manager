package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/imyousuf/fs-image-manager/app"
	"github.com/imyousuf/fs-image-manager/app/controllers"
	"github.com/imyousuf/fs-image-manager/app/services"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	// Read configuration
	configLoc := flag.String("config", app.DefaultConfigFilePath, "Location of the configuration file")
	flag.Parse()
	config, confErr := app.GetConfiguration(*configLoc)
	setupLogger(config)
	if confErr != nil {
		log.Panic("Configuration error", confErr)
	}
	// Initialize DB Connection
	log.Println("DB Connection URL -", config.GetDBConnectionURL())
	dbConnSetup := func() error {
		if services.InitAndCheckDBConnection(config) {
			return nil
		} else {
			services.ReInitDBConnection()
			return errors.New("Connection not established")
		}
	}
	eBackoff := backoff.NewConstantBackOff(20 * time.Second)
	timeoutContext, timeoutCancelFunc := context.WithTimeout(context.Background(), 5*time.Minute)
	defer timeoutCancelFunc()
	backoffContext := backoff.WithContext(eBackoff, timeoutContext)
	backoff.Retry(dbConnSetup, backoffContext)
	if !services.InitAndCheckDBConnection(config) {
		log.Fatal(errors.New("Could not initialize DB connection within 5 mins!"))
	}
	// Setup HTTP Server
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	server := controllers.ConfigureWebAPI(config, config)
	go func() {
		log.Println("Listening to http at -", config.GetHTTPListeningAddr())
		fmt.Println("Listening to http at -", config.GetHTTPListeningAddr())
		if serverListenErr := server.ListenAndServe(); serverListenErr != nil {
			log.Fatal(serverListenErr)
		}
	}()
	<-stop
	handleExit(server)
}

func setupLogger(config app.LogConfig) {
	if config.IsLoggerConfigAvailable() {
		log.SetOutput(&lumberjack.Logger{
			Filename:   config.GetLogFilename(),
			MaxSize:    config.GetMaxLogFileSize(), // megabytes
			MaxBackups: config.GetMaxLogBackups(),
			MaxAge:     config.GetMaxAgeForALogFile(),             //days
			Compress:   config.IsCompressionEnabledOnLogBackups(), // disabled by default
		})
	}
}

func handleExit(server *http.Server) {
	log.Println("Shutting down the server...")
	serverShutdownContext, shutdownTimeoutCancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownTimeoutCancelFunc()
	server.Shutdown(serverShutdownContext)
	log.Println("Server gracefully stopped!")
}
