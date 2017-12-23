import { WebAPI } from "./web-api";
import { EventAggregator } from 'aurelia-event-aggregator';
import { ImageClickedOn } from "./messages";

export class Download {
    static inject = [WebAPI, EventAggregator]
    selectedImages = [];
    downloadURI = "";
    disableButtons = true;
    constructor(api, ea) {
        this.api = api;
        this.ea = ea;
        this.ea.subscribe(ImageClickedOn, msg => {
            let img = msg.image
            let foundIndex = -1;
            for (let index = 0; index < this.selectedImages.length; ++index) {
                if (img.DownloadID == this.selectedImages[index].DownloadID) {
                    foundIndex = index;
                }
            }
            if (foundIndex > -1) {
                this.selectedImages.splice(foundIndex, 1);
            } else {
                this.selectedImages.push(img);
            }
            if (this.selectedImages.length > 0) {
                this.disableButtons = false;
            } else {
                this.disableButtons = true;
            }
        });
    }

    bind() {
        if (this.api.isInitialized()) {
            this.downloadURI = this.downloadImagesURI;
        } else {
            let self = this;
            this.api.getDownloadURI().then(downloadURI => {
                self.downloadURI = downloadURI;
            });
        }
    }

    clearSelection() {
        this.selectedImages.splice(0, this.selectedImages.length);
        this.disableButtons = true;
        return true;
    }
}