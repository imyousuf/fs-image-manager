import { WebAPI } from './web-api';
import { EventAggregator } from 'aurelia-event-aggregator';
import { DirectoryClicked, ImageClickedOn } from './messages'

export class RootMedia {
    static inject = [WebAPI, EventAggregator]
    rootListing = {}
    constructor(api, ea) {
        this.api = api;
        this.ea = ea
        this.message = 'Hello world';
    }

    bind() {
        let self = this
        self.api.getRootMedia().then(rootMedia => {
            self.rootListing = rootMedia;
        });
    }

    clickDir(dir) {
        this.ea.publish(new DirectoryClicked(dir))
        return true;
    }
}