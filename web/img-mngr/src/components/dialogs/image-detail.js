import { DialogController } from 'aurelia-dialog';
import { DOM } from 'aurelia-pal';

export class ImageDetail {
    static inject = [DialogController];
    image = { Name: '', ImageURL: '', ThumbnailURL: '', DownloadID: '', next: null, previous: null };
    constructor(controller) {
        this.controller = controller;
        this.image = null;
        let self = this;
        this.keyboardEventHandler = (keyboardEvent) => {
            // console.log('KeyBoard Event for image scrolling');
            if (keyboardEvent.keyCode === 39) {
                self.next();
            } else if (keyboardEvent.keyCode === 37) {
                self.previous();
            }
        };
    }

    activate(image) {
        this.image = image;
        DOM.addEventListener('keyup', this.keyboardEventHandler, false);
    }

    deactivate() {
        DOM.removeEventListener('keyup', this.keyboardEventHandler, false);
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
