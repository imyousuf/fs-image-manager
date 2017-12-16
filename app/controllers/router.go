package controllers

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/imyousuf/fs-image-manager/app"
)

const (
	webAppURLPrefix       = "/web/"
	apiURLPrefix          = "/api"
	contentTypeHeaderName = "Content-Type"
	jsonMIMEType          = "application/json"
)

var (
	router            *mux.Router
	apiRouter         *mux.Router
	routerInitializer sync.Once
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
	routerInitializer.Do(func() {
		router = mux.NewRouter()
		setupNonAPIRoutes(router, config)
		apiRouter = router.PathPrefix(apiURLPrefix).Subrouter()
		setupAPIRoutes(apiRouter)
	})
	server := &http.Server{
		Handler:      handlers.LoggingHandler(RequestLogger{}, router),
		Addr:         config.GetHTTPListeningAddr(),
		ReadTimeout:  time.Duration(config.GetHTTPReadTimeout()) * time.Second,
		WriteTimeout: time.Duration(config.GetHTTPWriteTimeout()) * time.Second,
	}
	return server
}

func setupNonAPIRoutes(router *mux.Router, config app.HTTPConfig) {
	router.HandleFunc("/", apiDefaultHandler)
	router.PathPrefix(webAppURLPrefix).Handler(http.StripPrefix(webAppURLPrefix,
		http.FileServer(http.Dir(config.GetStaticFileDir()))))
}

func setupAPIRoutes(apiRouter *mux.Router) {
	apiRouter.HandleFunc("/", apiRootHandler)
	apiRouter.HandleFunc(apiAccessURLPattern, apiAccessHandler).Methods("GET").
		Name(apiAccessRouteName)
	apiRouter.HandleFunc(downloadHistoryURLPattern, downloadHistoryHandler).
		Methods("GET").Name(downloadHistoryRouteName)
	apiRouter.HandleFunc(downloadURLPattern, downloadHandler).Methods("POST").
		Name(downloadRouteName)
	apiRouter.HandleFunc(listMediaURLPattern, listMediaRootHandler).Methods("Get").
		Name(listMediaName)
	apiRouter.HandleFunc(listMediaURLPattern, listMediaHandler).Methods("Get").
		Queries("path", "{dirPath}")
}
