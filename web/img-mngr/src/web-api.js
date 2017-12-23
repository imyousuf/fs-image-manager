import { HttpClient } from 'aurelia-http-client';



function getPrimaryResources() {
    this.isRequesting = true
    return this.http.get("/api/access")
}

export class WebAPI {
    static inject = [HttpClient]
    isRequesting = false;
    constructor(http) {
        this.http = http;
        this.mediaURI = ""
        this.downloadHistoryURI = ""
        this.downloadImagesURI = ""
        this.initialized = false
    }

    isInitialized() {
        return this.initialized
    }

    init() {
        let self = this
        return getPrimaryResources.bind(this)().then(data => {
            if (self.initialized) {
                return
            }
            let response = JSON.parse(data.response)
            self.mediaURI = response.MediaURI
            self.downloadHistoryURI = response.DownloadHistoryURI
            self.downloadImagesURI = response.DownloadImagesURI
            self.isRequesting = false
            self.initialized = true
        });
    }

    getRootMedia() {
        let loadRootMediaPromise = this._getWrapperPromise();
        let self = this
        return loadRootMediaPromise.then(() => {
            return self.http.get(self.mediaURI).then(data => {
                return JSON.parse(data.response);
            });
        });
    }

    _getWrapperPromise() {
        let initPromise;
        if (this.isInitialized()) {
            initPromise = new Promise((resolve) => {
                resolve("done");
            });
        } else {
            initPromise = this.init();
        }
        return initPromise
    }

    getPathMedia(path) {
        let loadRootMediaPromise = this._getWrapperPromise();
        let self = this
        return loadRootMediaPromise.then(() => {
            return self.http.get(path).then(data => {
                return JSON.parse(data.response);
            });
        });
    }

    getDownloadURI() {
        let initPromise = this._getWrapperPromise();
        let self = this;
        return initPromise.then(() => {
            return self.downloadImagesURI
        });
    }
}