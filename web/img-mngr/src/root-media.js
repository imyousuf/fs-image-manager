import { WebAPI } from './web-api';
import { EventAggregator } from 'aurelia-event-aggregator';
import { DirectoryClicked } from './messages'

export class RootMedia {
    static inject = [WebAPI, EventAggregator]
    rootListing = {}
    constructor(api, ea) {
        this.api = api;
        this.ea = ea;
    }

    bind() {
        let self = this
        self.api.getRootMedia().then(rootMedia => {
            self.rootListing = rootMedia;
        });
    }
}