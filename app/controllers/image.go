package controllers

import (
	"net/http"

	"github.com/nfnt/resize"
)

const (
	imageScaleURLPattern       = "/image-scale"
	imageScaleRouteName        = "image-scale"
	imageScaleWidthQueryParam  = "width"
	imageScaleHeightQueryParam = "height"
	imageScalePathQueryParam   = "path"
	defaultHeight              = 700
)

func imageScalingHandler(w http.ResponseWriter, r *http.Request) {
	resize.Resize(0, defaultHeight, nil, resize.Lanczos3)
}
