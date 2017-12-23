import { WebAPI } from './web-api';
import { EventAggregator } from 'aurelia-event-aggregator';
import { HomeClicked } from './messages'

export class App {
    static inject = [WebAPI, EventAggregator]
    constructor(api, ea) {
        this.title = 'Image Manager';
        this.ea = ea
        this.api = api
    }

    configureRouter(config, router) {
        config.title = 'Images';
        config.map([
            { route: '', moduleId: 'root-media', title: 'Root', name: 'rootPath' },
            { route: 'path', moduleId: 'path-media', title: 'Path', name: 'path' }
        ]);
    }

    clickHome() {
        this.ea.publish(new HomeClicked());
        return true;
    }
}