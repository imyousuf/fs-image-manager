package controllers

import (
	"net/http"
)

const (
	apiAccessURLPattern = "/access"
)

func apiDefaultHandler(w http.ResponseWriter, r *http.Request) {
	newURL := r.URL
	newURL.Host = r.Host
	newURL.Path = "/web/index.html"
	http.Redirect(w, r, newURL.String(), http.StatusSeeOther)
}

func apiAccessHandler(w http.ResponseWriter, r *http.Request) {

}

func apiRootHandler(w http.ResponseWriter, r *http.Request) {
	newURL := r.URL
	newURL.Host = r.Host
	newURL.Path = apiAccessURLPattern
	http.Redirect(w, r, newURL.String(), http.StatusSeeOther)
}
