import { DialogController } from 'aurelia-dialog';
import { DOM } from 'aurelia-pal';
import { ImageClickedOn } from '../../messages';
import { EventAggregator } from 'aurelia-event-aggregator';


export class ImageDetail {
    static inject = [DialogController, EventAggregator];
    image = { Name: '', ImageURL: '', ThumbnailURL: '', DownloadID: '', next: null, previous: null, Selected: false };
    constructor(controller, ea) {
        this.controller = controller;
        this.image = null;
        this.ea = ea;
        let self = this;
        this.keyboardEventHandler = (keyboardEvent) => {
            // console.log('KeyBoard Event for image scrolling');
            if (keyboardEvent.keyCode === 39) {
                self.next();
            } else if (keyboardEvent.keyCode === 37) {
                self.previous();
            } else if (keyboardEvent.keyCode === 32) {
                self.invertSelection();
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

    invertSelection() {
        this.image.Selected = !this.image.Selected;
        this.selectorClicked();
    }

    selectorClicked() {
        this.ea.publish(new ImageClickedOn(this.image));
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
