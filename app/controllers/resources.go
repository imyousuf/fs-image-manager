package controllers

// RootResource represents the successful response to a "/access" API
type RootResource struct {
	MediaURI           string
	DownloadHistoryURI string
	DownloadImagesURI  string
}

// DirectoryResource represents a folder or collection of files
type DirectoryResource struct {
	Name    string
	ListURI string
}

// ImageResource represents a single Image Resource
type ImageResource struct {
	Name     string
	ImageURL string
}

// ListResource represents the data sent on a list call
type ListResource struct {
	Directories []DirectoryResource
	Images      []ImageResource
}
