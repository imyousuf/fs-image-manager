import { DirectoryClicked, ImageClickedOn, ViewPaneChangeCompleted, CurrentSelection } from './messages'
import { EventAggregator } from 'aurelia-event-aggregator';
import Blazy from "blazy";

export class ListItems {
    static inject = [EventAggregator]
    constructor(ea) {
        this.media = {};
        this.ea = ea;
        let self = this;
        this.ea.subscribe(CurrentSelection, msg => {
            if (self.media.Images) {
                let selectedImages = msg.selectedImages;
                let model = self;
                for (var index = 0; index < model.media.Images.length; ++index) {
                    let image = model.media.Images[index];
                    let imageFound = false;
                    for (var sIndex = 0; sIndex < selectedImages.length; ++sIndex) {
                        if (image.DownloadID == selectedImages[sIndex].DownloadID) {
                            image.Selected = true;
                            imageFound = true;
                        }
                    }
                    if (!imageFound) {
                        image.Selected = false;
                    }
                }
            }
        });
    }
    activate(model) {
        if (model.media.Directories) {
            for (var index = 0; index < model.media.Directories.length; ++index) {
                let directory = model.media.Directories[index];
                directory.EncodedListURI = encodeURIComponent(directory.ListURI);
            };
        }
        if (model.media.Images) {
            for (var index = 0; index < model.media.Images.length; ++index) {
                let image = model.media.Images[index];
                image.Selected = false;
            }
        }
        this.media = model.media
        let self = this;
        setTimeout(() => {
            self.blazy = new Blazy({
                src: 'data-blazy'
            });
            self.ea.publish(new ViewPaneChangeCompleted());
        }, 100);
    }
    detached() {
        this.blazy.destroy();
    }

    deactivate() {
        this.detached();
    }

    clickImage(img) {
        img.Selected = !img.Selected;
        this.ea.publish(new ImageClickedOn(img));
        return true;
    }

    clickDir(dir) {
        this.ea.publish(new DirectoryClicked(dir));
        return true;
    }
}