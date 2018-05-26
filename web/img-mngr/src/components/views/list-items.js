import { DirectoryClicked, ImageClickedOn, ViewPaneChangeCompleted, CurrentSelection } from '../../messages';
import { EventAggregator } from 'aurelia-event-aggregator';
import { DialogService } from 'aurelia-dialog';
import { ImageDetail } from '../dialogs/image-detail';
import Blazy from 'blazy';

export class ListItems {
    static inject = [EventAggregator, DialogService]
    constructor(ea, dialogService) {
        this.media = {};
        this.ea = ea;
        this.dialogService = dialogService;
        this._setupSubscriber();
    }

    _setupSubscriber() {
        let self = this;
        this.ea.subscribe(CurrentSelection, msg => {
            if (self.media.Images) {
                let selectedImages = msg.selectedImages;
                let model = self;
                for (let index = 0; index < model.media.Images.length; ++index) {
                    let image = model.media.Images[index];
                    // Check if the image is an already selected image
                    let imageFound = false;
                    for (let sIndex = 0; sIndex < selectedImages.length; ++sIndex) {
                        if (image.DownloadID === selectedImages[sIndex].DownloadID) {
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
            for (let index = 0; index < model.media.Directories.length; ++index) {
                let directory = model.media.Directories[index];
                directory.EncodedListURI = encodeURIComponent(directory.ListURI);
            }
        }
        if (model.media.Images) {
            for (let index = 0; index < model.media.Images.length; ++index) {
                let image = model.media.Images[index];
                image.Selected = false;
                // Set previous node
                if (index - 1 < 0) {
                    image.previous = null;
                } else {
                    image.previous = model.media.Images[index - 1];
                }
                // Set next node
                if (index + 1 >= model.media.Images.length) {
                    image.next = null;
                } else {
                    image.next = model.media.Images[index + 1];
                }
            }
        }
        this.media = model.media;
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

    doubleClickImage(img) {
        this.dialogService.open({ viewModel: ImageDetail, model: img, lock: false, position: (modalContainer, modalOverlay) => {
            modalContainer.scrollIntoView();
        } }).whenClosed(response => {
            if (!response.wasCancelled) {
                console.log('Image dialog closed');
            } else {
                console.log('Unexpected image dialog close');
            }
            console.log(response.output);
        });
        return true;
    }

    clickDir(dir) {
        this.ea.publish(new DirectoryClicked(dir));
        return true;
    }
}
