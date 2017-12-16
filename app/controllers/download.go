package controllers

import (
	"net/http"
	"net/url"
)

const (
	downloadURLPattern        = "/download"
	downloadRouteName         = "download"
	downloadHistoryURLPattern = "/download-history/{deviceID}"
	downloadHistoryRouteName  = "download-history"
)

func downloadHandler(w http.ResponseWriter, r *http.Request) {
}

func downloadHistoryHandler(w http.ResponseWriter, r *http.Request) {
}

func getDownloadURL() (*url.URL, error) {
	return apiRouter.Get(downloadRouteName).URLPath()
}

func getDownloadHistoryURL(deviceID string) (*url.URL, error) {
	return apiRouter.Get(downloadHistoryRouteName).URLPath("deviceID", deviceID)
}
