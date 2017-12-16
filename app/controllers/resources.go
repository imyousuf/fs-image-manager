package controllers

// RootResource represents the successful response to a "/access" API
type RootResource struct {
	MediaURI           string
	DownloadHistoryURI string
	DownloadImagesURI  string
}
