package controllers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/imyousuf/fs-image-manager/app/services"

	"github.com/gorilla/mux"
)

const (
	downloadDeviceParamName      = "deviceID"
	downloadURLPattern           = "/download/{" + downloadDeviceParamName + "}"
	downloadRouteName            = "download"
	downloadHistoryURLPattern    = "/download-history/{" + downloadDeviceParamName + "}"
	downloadHistoryRouteName     = "download-history"
	downloadPathFormParamName    = "downloadFile"
	zipMIMEType                  = "application/zip"
	contentDispositionHeaderName = "Content-Disposition"
	contentDispositionFMTPattern = "attachment; filename=\"%s\""
)

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	pathVars := mux.Vars(r)
	deviceID := pathVars[downloadDeviceParamName]
	device, found := services.GetDeviceByDeviceID(deviceID)
	// Check if we recognize the device
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// Check if we are able to parse the form data
	err := r.ParseForm()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	// Make sure the files requested for download is available
	files := r.PostForm[downloadPathFormParamName]
	fileMap := make(map[string]string)
	for _, relativeFileName := range files {
		fPath := libraryConfig.GetLibraryRoot() + relativeFileName
		if fi, err := os.Stat(fPath); err != nil || fi.IsDir() {
			errString := "Can not download directory"
			if os.IsNotExist(err) {
				errString = relativeFileName + ": no such file or directory"
			} else {
				errString = err.Error()
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(errString))
			return
		}
		fileMap[fPath] = relativeFileName[1:]
	}
	// Record the download in system
	download, err := device.RecordDownload(files)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	// Write status and headers
	attachmentName := device.GetDeviceID() + "_" + strconv.FormatUint(uint64(download.GetDownloadID()), 10) + ".zip"
	w.Header().Set(contentDispositionHeaderName, fmt.Sprintf(contentDispositionFMTPattern, attachmentName))
	w.Header().Set(contentTypeHeaderName, zipMIMEType)
	w.WriteHeader(200)
	// Write ZIP file with contents
	zw := zip.NewWriter(w)
	for aPath, name := range fileMap {
		if fw, err := zw.Create(name); err == nil {
			if fr, err := os.Open(aPath); err == nil {
				io.Copy(fw, fr)
			} else {
				log.Println(err)
			}
		} else {
			log.Println(err)
		}
	}
	zw.Close()
}

func downloadHistoryHandler(w http.ResponseWriter, r *http.Request) {
	pathVars := mux.Vars(r)
	deviceID := pathVars[downloadDeviceParamName]
	device, found := services.GetDeviceByDeviceID(deviceID)
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	downloads := device.GetDownloads()
	downloadRsrc := make([]DownloadHistoryResource, 0, len(downloads))
	for _, download := range downloads {
		downloadRsrc = append(downloadRsrc, DownloadHistoryResource{
			DownloadedAt:    download.GetDownloadPerformedAt(),
			DownloadedFiles: download.GetDownloadedFiles()})
	}
	w.Header().Set(contentTypeHeaderName, jsonMIMEType)
	json.NewEncoder(w).Encode(&downloadRsrc)
}

func getDownloadURL(deviceID string) (*url.URL, error) {
	return apiRouter.Get(downloadRouteName).URLPath(downloadDeviceParamName, deviceID)
}

func getDownloadHistoryURL(deviceID string) (*url.URL, error) {
	return apiRouter.Get(downloadHistoryRouteName).URLPath(downloadDeviceParamName, deviceID)
}
