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
        this.media = model.media
    }
    attached() {
        console.log(window.jQuery);
        setTimeout(() => {
            this.blazy = new Blazy({
                src: 'data-blazy'
            });
        }, 100);
    }
    clickImage(img) {
        this.ea.publish(new ImageClickedOn(img));
        return true;
    }

    clickDir(dir) {
        this.ea.publish(new DirectoryClicked(dir));
        //FIXME: this should not be required
        this.attached();
        return true;
    }
}