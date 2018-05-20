import { DialogController } from 'aurelia-dialog';

export class ImageDetail {
    static inject = [DialogController];
    image = { Name: '', ImageURL: '', ThumbnailURL: '', DownloadID: '' };
    constructor(controller) {
        this.controller = controller;
        this.image = null;
    }

    activate(image) {
        this.image = image;
    }
}
