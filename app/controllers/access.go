package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/imyousuf/fs-image-manager/app/services"
)

const (
	apiAccessURLPattern = "/access"
	apiAccessRouteName  = "access"
	// DeviceCookieName represents the name of the cookie used to store the device identifier
	DeviceCookieName = "__dID"
)

func apiDefaultHandler(w http.ResponseWriter, r *http.Request) {
	newURL := r.URL
	newURL.Host = r.Host
	newURL.Path = "/web/index.html"
	http.Redirect(w, r, newURL.String(), http.StatusSeeOther)
}

func apiAccessHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(DeviceCookieName)
	var device *services.Device
	if err != nil {
		device = handleNewDevice(w, r)
	} else {
		var found bool
		device, found = services.GetDevice(cookie.Value)
		if !found {
			device = handleNewDevice(w, r)
		} else {
			if !device.IsDeviceCookieStillValid() {
				updateErr := device.UpdateDeviceWithNewCookie()
				if updateErr == nil {
					setCookie(w, device)
				}
			}
		}
	}
	sendURIsForDiscovery(device, w, r)
}

func sendURIsForDiscovery(device *services.Device, w http.ResponseWriter, r *http.Request) {
	w.Header().Set(contentTypeHeaderName, jsonMIMEType)
	downloadHistoryURL, _ := getDownloadHistoryURL(device.GetDeviceID())
	downloadURL, _ := getDownloadURL()
	rootResource := &RootResource{DownloadHistoryURI: downloadHistoryURL.String(),
		DownloadImagesURI: downloadURL.String()}
	json.NewEncoder(w).Encode(*rootResource)
}

func setCookie(w http.ResponseWriter, device *services.Device) {
	http.SetCookie(w, &http.Cookie{Name: DeviceCookieName,
		Value: device.GetCurrentCookieValue(), Expires: device.GetCurrentCookieValidTill(),
		Path: getAccessURL()})
}

func handleNewDevice(w http.ResponseWriter, r *http.Request) *services.Device {
	device := services.CreateDevice()
	setCookie(w, device)
	return device
}

func apiRootHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, getAccessURL(), http.StatusSeeOther)
}

func getAccessURL() string {
	newURL, _ := apiRouter.Get(apiAccessRouteName).URLPath()
	return newURL.String()
}
