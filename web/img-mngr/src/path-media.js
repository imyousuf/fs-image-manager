import { WebAPI } from './web-api';

export class PathMedia {
    static inject = [WebAPI]
    pathListing = {}
    constructor(api) {
        this.api = api;
        this.path = "";
        console.log("PC!")
    }

    activate(params) {
        this.path = params.path
        console.log("PA! " + this.path)
    }

    deactivate() {
        console.log("PD! " + this.path)
        this.pathListing = {}
        this.path = ""
    }

    bind() {
        console.log("PB! " + this.path)
        let self = this
        self.api.getPathMedia(self.path).then(pathMedia => {
            self.pathListing = pathMedia;
        });
    }

    click(dir) {
        this.path = dir.ListURI;
        this.bind();
        return true;
    }
}