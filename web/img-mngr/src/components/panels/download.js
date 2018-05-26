import { WebAPI } from '../../web-api';
import { EventAggregator } from 'aurelia-event-aggregator';
import { ImageClickedOn, ViewPaneChangeCompleted, CurrentSelection } from '../../messages';

export class Download {
    static inject = [WebAPI, EventAggregator]
    selectedImages = [];
    downloadURI = '';
    disableButtons = true;
    constructor(api, ea) {
        this.api = api;
        this.ea = ea;
        this._setupSubscribers();
    }

    _setupSubscribers() {
        let self = this;
        this.ea.subscribe(ImageClickedOn, msg => {
            let img = msg.image;
            let foundIndex = -1;
            for (let index = 0; index < self.selectedImages.length; ++index) {
                if (img.DownloadID === self.selectedImages[index].DownloadID) {
                    foundIndex = index;
                }
            }
            if (foundIndex > -1) {
                self.selectedImages.splice(foundIndex, 1);
            } else {
                self.selectedImages.push(img);
            }
            if (self.selectedImages.length > 0) {
                self.disableButtons = false;
            } else {
                self.disableButtons = true;
            }
        });
        this.ea.subscribe(ViewPaneChangeCompleted, msg => {
            self._publishSelection();
        });
    }

    _publishSelection() {
        if (this.selectedImages && this.selectedImages.length >= 0) {
            this.ea.publish(new CurrentSelection(this.selectedImages));
        }
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
        this._publishSelection();
        return true;
    }
}
