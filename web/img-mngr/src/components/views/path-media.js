import { WebAPI } from '../../web-api';
import { EventAggregator } from 'aurelia-event-aggregator';

export class PathMedia {
    static inject = [WebAPI, EventAggregator]
    pathListing = {}
    constructor(api, ea) {
        this.api = api;
        this.ea = ea;
        this.path = '';
    }

    activate(params) {
        this.path = decodeURIComponent(params.path);
        this.bind();
    }

    deactivate() {
        this.path = '';
    }

    bind() {
        let self = this;
        self.api.getPathMedia(self.path).then(pathMedia => {
            self.pathListing = pathMedia;
        });
    }

    unbind() {
        this.pathListing = {};
    }
}
