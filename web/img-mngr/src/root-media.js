import { WebAPI } from './web-api';

export class RootMedia {
    static inject = [WebAPI]
    rootListing = {}
    constructor(api) {
        this.api = api;
        this.message = 'Hello world';
    }

    bind() {
        let self = this
        self.api.getRootMedia().then(rootMedia => {
            self.rootListing = rootMedia;
        });
    }

    clickDir(dir) {
        return true;
    }
}