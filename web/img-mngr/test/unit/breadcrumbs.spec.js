import { EventAggregator } from 'aurelia-event-aggregator';
import { Breadcrumbs } from '../../src/components/panels/breadcrumbs';
import { DirectoryClicked, BackButtonClicked, HomeClicked } from '../../src/messages';

function _createDir(listURI) {
  return {ListURI: listURI};
}

describe('Breadcrumbs', ()=>{
  let breadcrumbsComp;
  let ea;

  beforeEach(() => {
    ea = new EventAggregator();
    breadcrumbsComp = new Breadcrumbs(ea);
  });

  it('should have 0 items in breadcrumb', () => {
    expect(breadcrumbsComp.dirs).toEqual([]);
  });

  it('should add 1 dir as item in breadcrumb', () => {
    let firstItem = _createDir('/a');
    ea.publish(new DirectoryClicked(firstItem));
    expect(breadcrumbsComp.dirs.length).toBe(1);
    expect(breadcrumbsComp.dirs).toEqual([firstItem]);
  });
  it('should remain 1 dir as item in breadcurmb after the first dir is clicked', () => {
    let firstItem = _createDir('/a');
    ea.publish(new DirectoryClicked(firstItem));
    let lastDir = _createDir('/a/b');
    ea.publish(new DirectoryClicked(lastDir));
    expect(breadcrumbsComp.dirs.length).toBe(2);
    breadcrumbsComp.clickBreadcrumb(firstItem);
    expect(breadcrumbsComp.dirs.length).toBe(1);
    expect(breadcrumbsComp.dirs).toEqual([firstItem]);
  });
  it('should remain 1 dir as item in breadcurmb after browser back-button is clicked', () => {
    let firstItem = _createDir('/a');
    ea.publish(new DirectoryClicked(firstItem));
    let lastDir = _createDir('/a/b');
    ea.publish(new DirectoryClicked(lastDir));
    expect(breadcrumbsComp.dirs.length).toBe(2);
    ea.publish(new BackButtonClicked());
    expect(breadcrumbsComp.dirs.length).toBe(1);
    expect(breadcrumbsComp.dirs).toEqual([firstItem]);
  });
  it('breadcrumb should be empty after home is clicked', () => {
    let firstItem = _createDir('/a');
    ea.publish(new DirectoryClicked(firstItem));
    let lastDir = _createDir('/a/b');
    ea.publish(new DirectoryClicked(lastDir));
    expect(breadcrumbsComp.dirs.length).toBe(2);
    ea.publish(new HomeClicked());
    expect(breadcrumbsComp.dirs).toEqual([]);
  });
  it('breadcrumb should be empty after home is clicked in the breadcrumb', () => {
    let firstItem = _createDir('/a');
    ea.publish(new DirectoryClicked(firstItem));
    let lastDir = _createDir('/a/b');
    ea.publish(new DirectoryClicked(lastDir));
    expect(breadcrumbsComp.dirs.length).toBe(2);
    breadcrumbsComp.clickHome();
    expect(breadcrumbsComp.dirs).toEqual([]);
  });
});
