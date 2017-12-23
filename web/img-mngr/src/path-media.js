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
        return true;
    }
}