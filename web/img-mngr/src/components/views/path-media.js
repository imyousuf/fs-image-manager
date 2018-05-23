import { WebAPI } from '../../web-api';
import { BackButtonClicked } from '../../messages';
import { EventAggregator } from 'aurelia-event-aggregator';
import { Router } from 'aurelia-router';

export class PathMedia {
    static inject = [WebAPI, EventAggregator, Router]
    pathListing = {}
    constructor(api, ea, router) {
        this.api = api;
        this.ea = ea;
        this.router = router;
        this.path = '';
    }

    activate(params) {
        this.path = decodeURIComponent(params.path);
        this.bind();
    }

    canDeactivate() {
        if (this.router.isNavigatingForward) {
            return false;
        }
        return true;
    }

    deactivate() {
        this.path = '';
        if (this.router.isNavigatingBack) {
            this.ea.publish(new BackButtonClicked());
        }
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
