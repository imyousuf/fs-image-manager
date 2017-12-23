import { WebAPI } from './web-api';
import { EventAggregator } from 'aurelia-event-aggregator';
import { DirectoryClicked, ImageClickedOn, BreadcrumbClicked } from './messages'

export class PathMedia {
    static inject = [WebAPI, EventAggregator]
    pathListing = {}
    constructor(api, ea) {
        this.api = api;
        this.ea = ea
        this.path = "";
        self = this;
        this.ea.subscribe(BreadcrumbClicked, msg => {
            self.clickDir(msg.directory)
        })
    }

    activate(params) {
        this.path = params.path
    }

    deactivate() {
        this.pathListing = {}
        this.path = ""
    }

    bind() {
        let self = this
        self.api.getPathMedia(self.path).then(pathMedia => {
            self.pathListing = pathMedia;
        });
    }

    clickDir(dir) {
        this.path = dir.ListURI;
        this.bind();
        this.ea.publish(new DirectoryClicked(dir))
        return true;
    }

    clickImage(img) {
        this.ea.publish(new ImageClickedOn(img))
        return true;
    }
}