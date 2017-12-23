import { EventAggregator } from 'aurelia-event-aggregator';
import { DirectoryClicked, HomeClicked, BreadcrumbClicked } from './messages'

export class Breadcrumbs {
    static inject = [EventAggregator]
    dirs = []
    constructor(ea) {
        this.ea = ea
        this.message = 'Hello world';
        ea.subscribe(DirectoryClicked, msg => {
            let dir = msg.directory;
            let found = false;
            let foundIndex = -1;
            for (let index = 0; index < this.dirs.length; ++index) {
                if (this.dirs[index].ListURI == dir.ListURI) {
                    found = true
                    foundIndex = index
                }
            }
            if (!found) {
                this.dirs.push(dir)
            } else {
                this.dirs.splice(foundIndex + 1, this.dirs.length - foundIndex - 1)
            }
        });
        ea.subscribe(HomeClicked, msg => {
            this.dirs.splice(0, this.dirs.length)
        });
    }

    clickBreadcrumb(dir) {
        this.ea.publish(new BreadcrumbClicked(dir))
        return true;
    }
}