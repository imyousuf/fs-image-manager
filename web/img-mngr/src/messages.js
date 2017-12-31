export class DirectoryClicked {
    constructor(directory) {
        this.directory = directory;
    }
}

export class ImageClickedOn {
    constructor(image) {
        this.image = image;
    }
}

export class HomeClicked {

}

export class BreadcrumbClicked {
    constructor(directory) {
        this.directory = directory;
    }
}

export class ViewPaneChangeCompleted {
    constructor() {}
}

export class CurrentSelection {
    constructor(selectedImages) {
        this.selectedImages = selectedImages;
    }
}