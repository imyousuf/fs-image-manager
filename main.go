package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/imyousuf/fs-image-manager/app/services"

	"github.com/imyousuf/fs-image-manager/app"
	"github.com/imyousuf/fs-image-manager/app/controllers"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	configLoc := flag.String("config", app.DefaultConfigFilePath, "Location of the configuration file")
	flag.Parse()
	config, confErr := app.GetConfiguration(*configLoc)
	setupLogger(config)
	if confErr != nil {
		log.Panic("Configuration error", confErr)
	}
	log.Println("DB Connection URL -", config.GetDBConnectionURL())
	services.InitAndCheckDBConnection(config)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	server := controllers.ConfigureWebAPI(config)
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
	serverShutdownContext, _ := context.WithTimeout(context.Background(), 5*time.Second)
	server.Shutdown(serverShutdownContext)
	log.Println("Server gracefully stopped!")
}
