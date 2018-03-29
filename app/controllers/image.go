package controllers

import (
	"image/jpeg"

	"github.com/disintegration/imageorient"
	// Import PNG package so that it can decode PNG images
	_ "image/png"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/nfnt/resize"
)

const (
	imageScaleURLPattern       = "/image-scale"
	imageScaleRouteName        = "image-scale"
	imageScaleWidthQueryParam  = "width"
	imageScaleHeightQueryParam = "height"
	imageScalePathQueryParam   = "path"
	defaultHeight              = 650
	defaultThumbnailHeight     = 110
)

func imageScalingHandler(w http.ResponseWriter, r *http.Request) {
	expectedVars := mux.Vars(r)
	imagePath := expectedVars[imageScalePathQueryParam]
	queryParams := r.URL.Query()
	widthStr := queryParams.Get(imageScaleWidthQueryParam)
	heightStr := queryParams.Get(imageScaleHeightQueryParam)
	log.Println("Image trying to scale:", imagePath, widthStr, heightStr)
	if imgFile, err := os.Open(libraryConfig.GetLibraryRoot() + imagePath); err == nil {
		if img, _, err := imageorient.Decode(imgFile); err == nil {
			var width, height int
			var iErr error
			if width, iErr = strconv.Atoi(widthStr); iErr != nil {
				width = 0
			}
			if height, iErr = strconv.Atoi(heightStr); iErr != nil {
				if width <= 0 {
					height = defaultHeight
				}
			}
			scaledImg := resize.Resize(uint(width), uint(height), img, resize.Lanczos3)
			w.Header().Set(contentTypeHeaderName, jpegMIMEType)
			w.WriteHeader(http.StatusOK)
			jpeg.Encode(w, scaledImg, nil)
		} else {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
		}
	} else {
		log.Println(err)
		w.WriteHeader(http.StatusNotFound)
	}
}

func getDefaultImageURL(imagePath string) string {
	newURL, _ := apiRouter.Get(imageScaleRouteName).URL(imageScalePathQueryParam, imagePath)
	return newURL.String()
}

func getThumbnailURL(imagePath string) string {
	newURL, _ := apiRouter.Get(imageScaleRouteName).
		URL(imageScalePathQueryParam, imagePath)
	return newURL.String() + "&" + imageScaleHeightQueryParam + "=" + strconv.Itoa(defaultThumbnailHeight)
}
