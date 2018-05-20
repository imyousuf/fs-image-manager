import { WebAPI } from '../../web-api';
import { EventAggregator } from 'aurelia-event-aggregator';

export class RootMedia {
    static inject = [WebAPI, EventAggregator]
    rootListing = {}
    constructor(api, ea) {
        this.api = api;
        this.ea = ea;
    }

    bind() {
        let self = this;
        self.api.getRootMedia().then(rootMedia => {
            self.rootListing = rootMedia;
        });
    }
}
