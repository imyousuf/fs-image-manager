package controllers

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"

	"github.com/imyousuf/fs-image-manager/app"
)

const (
	listMediaURLPattern     = "/list"
	listMediaName           = "list-media"
	listPathMediaURLPattern = "/list-path"
	listPathMediaName       = "list-path-media"
	dirPathQueryParam       = "path"
	dirPathQueryParamKey    = "dirPath"
)

var libraryConfig app.LibraryConfig

func getSupportedImageSuffixes() []string {
	supportedSuffixes := []string{".jpg", ".jpeg"}
	if libraryConfig.IsPNGSupported() {
		supportedSuffixes = append(supportedSuffixes, ".png")
	}
	return supportedSuffixes
}

func listDirectoriesAndImagesInPath(path string) ([]os.FileInfo, error) {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		supportedSuffixes := getSupportedImageSuffixes()
		filteredFiles := make([]os.FileInfo, 0, len(files))
		for _, file := range files {
			if file.IsDir() {
				filteredFiles = append(filteredFiles, file)
				continue
			}
			normalizedName := strings.ToLower(file.Name())
			for _, suffix := range supportedSuffixes {
				if strings.HasSuffix(normalizedName, suffix) {
					filteredFiles = append(filteredFiles, file)
				}
			}
		}
		return filteredFiles, err
	}
	return files, err
}

func getListResource(path string, files []os.FileInfo) *ListResource {
	directories := make([]DirectoryResource, 0, len(files))
	images := make([]ImageResource, 0, len(files))
	for _, file := range files {
		if file.IsDir() {
			directories = append(directories, DirectoryResource{Name: file.Name(),
				ListURI: getFolderListURI(path[len(libraryConfig.GetLibraryRoot()):] + file.Name() + "/")})
		} else {
			images = append(images, ImageResource{Name: file.Name(), ImageURL: "NoURL"})
		}
	}
	return &ListResource{Directories: directories, Images: images}
}

func listMediaRootHandler(w http.ResponseWriter, r *http.Request) {
	rootPath := libraryConfig.GetLibraryRoot() + "/"
	listDirectory(w, r, rootPath)
}

func listDirectory(w http.ResponseWriter, r *http.Request, path string) {
	files, err := listDirectoriesAndImagesInPath(path)
	if err == nil {
		listResource := getListResource(path, files)
		json.NewEncoder(w).Encode(*listResource)
	} else {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
	}
}

func getFolderListURI(path string) string {
	newURL, _ := apiRouter.Get(listPathMediaName).URL(dirPathQueryParamKey, path)
	return newURL.String()
}

func listMediaHandler(w http.ResponseWriter, r *http.Request) {
	routeVars := mux.Vars(r)
	dirPath := libraryConfig.GetLibraryRoot() + routeVars[dirPathQueryParamKey]
	listDirectory(w, r, dirPath)
}
