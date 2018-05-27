import { EventAggregator } from 'aurelia-event-aggregator';
import { Download } from '../../src/components/panels/download';
import { ImageClickedOn } from "../../src/messages";

describe('Download', () => {
  let sut, ea, api;

  beforeEach(() => {
    ea = new EventAggregator();
    api = {};
    sut = new Download(api, ea);
  });

  it('should have 0 selected images', () => {
    expect(sut.selectedImages).toEqual([]);
  });

  it('should select the image', () => {
    let msg = { image: { DownloadId: '123' }};
    ea.publish(new ImageClickedOn(msg));
    expect(sut.selectedImages.length).toBe(1);
  });

  it('should enable download button', () => {
    let msg = { image: { DownloadId: '123' }};
    expect(sut.disableButtons).toBe(true);
    ea.publish(new ImageClickedOn(msg));
    expect(sut.disableButtons).toBe(false);
  });

})
