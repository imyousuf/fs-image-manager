import { EventAggregator } from 'aurelia-event-aggregator';
import { Download } from '../../src/components/panels/download';
import { ImageClickedOn } from '../../src/messages';

describe('Download', () => {
  let downloadComp;
  let ea;
  let api;

  beforeEach(() => {
    ea = new EventAggregator();
    api = {};
    downloadComp = new Download(api, ea);
  });

  it('should have 0 selected images', () => {
    expect(downloadComp.selectedImages).toEqual([]);
  });

  it('should select the image', () => {
    let msg = { image: { DownloadId: '123' }};
    ea.publish(new ImageClickedOn(msg));
    expect(downloadComp.selectedImages.length).toBe(1);
  });

  it('should enable download button', () => {
    let msg = { image: { DownloadId: '123' }};
    expect(downloadComp.disableButtons).toBe(true);
    ea.publish(new ImageClickedOn(msg));
    expect(downloadComp.disableButtons).toBe(false);
  });
});
