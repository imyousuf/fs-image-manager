import { WebAPI } from './web-api';
export class App {
    static inject = [WebAPI]
    constructor(api) {
        this.title = 'Image Manager';
        this.api = api
    }

    configureRouter(config, router) {
        config.title = 'Images';
        config.map([
            { route: '', moduleId: 'root-media', title: 'Root', name: 'rootPath' },
            { route: 'path', moduleId: 'path-media', title: 'Path', name: 'path' }
        ]);
    }
}