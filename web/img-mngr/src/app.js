export class App {
    constructor() {
        this.message = 'Hello World!';
    }

    configureRouter(config, router) {
        config.title = 'Images';
        config.map([
            { route: '', moduleId: 'root-media', title: 'Root media' }
        ]);
    }
}