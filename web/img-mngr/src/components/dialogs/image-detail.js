import { DialogController } from 'aurelia-dialog';

export class ImageDetail {
    static inject = [DialogController];
    image = { Name: '', ImageURL: '', ThumbnailURL: '', DownloadID: '', next: null, previous: null };
    constructor(controller) {
        this.controller = controller;
        this.image = null;
    }

    activate(image) {
        this.image = image;
    }

    next() {
        if (this.image.next) {
            this.image = this.image.next;
        }
    }

    previous() {
        if (this.image.previous) {
            this.image = this.image.previous;
        }
    }
}
