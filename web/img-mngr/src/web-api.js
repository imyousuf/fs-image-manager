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
        let loadRootMediaPromise;
        let self = this
        if (this.isInitialized()) {
            loadRootMediaPromise = new Promise((resolve) => {
                resolve("done");
            });
        } else {
            loadRootMediaPromise = this.init();
        }
        return loadRootMediaPromise.then(() => {
            return self.http.get(self.mediaURI).then(data => {
                return JSON.parse(data.response);
            });
        });
    }

    getPathMedia(path) {
        let loadRootMediaPromise;
        let self = this
        if (this.isInitialized()) {
            loadRootMediaPromise = new Promise((resolve) => {
                resolve("done");
            });
        } else {
            loadRootMediaPromise = this.init();
        }
        return loadRootMediaPromise.then(() => {
            return self.http.get(path).then(data => {
                return JSON.parse(data.response);
            });
        });
    }
}