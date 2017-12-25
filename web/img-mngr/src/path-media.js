import { WebAPI } from './web-api';
import { EventAggregator } from 'aurelia-event-aggregator';
import { DirectoryClicked, BreadcrumbClicked } from './messages'

export class PathMedia {
    static inject = [WebAPI, EventAggregator]
    pathListing = {}
    constructor(api, ea) {
        this.api = api;
        this.ea = ea
        this.path = "";
        self = this;
        this.ea.subscribe(BreadcrumbClicked, msg => {
            self.dirClicked(msg.directory);
        });
        //FIXME: this should not be required
        this.ea.subscribe(DirectoryClicked, msg => {
            self.dirClicked(msg.directory);
        });
    }

    activate(params) {
        this.path = params.path
    }

    deactivate() {
        this.pathListing = {}
        this.path = ""
    }

    //FIXME: this should not be required
    dirClicked(directory) {
        this.path = directory.ListURI;
        this.bind();
    }

    bind() {
        let self = this
        self.api.getPathMedia(self.path).then(pathMedia => {
            self.pathListing = pathMedia;
        });
    }
}