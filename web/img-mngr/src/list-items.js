import { DirectoryClicked, ImageClickedOn } from './messages'
import { EventAggregator } from 'aurelia-event-aggregator';
import Blazy from "blazy";

export class ListItems {
    static inject = [EventAggregator]
    constructor(ea) {
        this.media = {}
        this.ea = ea
    }
    activate(model) {
        if (model.media.Directories) {
            for (var index = 0; index < model.media.Directories.length; ++index) {
                let directory = model.media.Directories[index];
                directory.EncodedListURI = encodeURIComponent(directory.ListURI);
            };
        }
        this.media = model.media
        setTimeout(() => {
            this.blazy = new Blazy({
                src: 'data-blazy'
            });
        }, 100);
    }
    detached() {
        this.blazy.destroy();
    }

    deactivate() {
        this.detached();
    }

    clickImage(img) {
        this.ea.publish(new ImageClickedOn(img));
        return true;
    }

    clickDir(dir) {
        this.ea.publish(new DirectoryClicked(dir));
        return true;
    }
}