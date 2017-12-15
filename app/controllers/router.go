package controllers

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/imyousuf/fs-image-manager/app"
)

const (
	webAppURLPrefix = "/web/"
)

// RequestLogger is a simple io.Writer that allows requests to be logged
type RequestLogger struct {
}

func (rLogger RequestLogger) Write(p []byte) (n int, err error) {
	log.Println(string(p))
	return len(p), nil
}

// ConfigureWebAPI configures all the backend API of the service
func ConfigureWebAPI(config app.HTTPConfig) *http.Server {
	router := mux.NewRouter()
	router.HandleFunc("/", apiDefaultHandler)
	router.PathPrefix(webAppURLPrefix).Handler(http.StripPrefix(webAppURLPrefix,
		http.FileServer(http.Dir(config.GetStaticFileDir()))))
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.HandleFunc("/", apiRootHandler)
	apiRouter.HandleFunc(apiAccessURLPattern, apiAccessHandler).Methods("GET")
	server := &http.Server{
		Handler:      handlers.LoggingHandler(RequestLogger{}, router),
		Addr:         config.GetHTTPListeningAddr(),
		ReadTimeout:  time.Duration(config.GetHTTPReadTimeout()) * time.Second,
		WriteTimeout: time.Duration(config.GetHTTPWriteTimeout()) * time.Second,
	}
	return server
}
