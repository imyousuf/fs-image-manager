import { WebAPI } from './web-api';
import { EventAggregator } from 'aurelia-event-aggregator';
import { DirectoryClicked } from './messages'

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
}