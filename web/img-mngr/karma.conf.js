'use strict';
const path = require('path');
const project = require('./aurelia_project/aurelia.json');

let testSrc = [
  { pattern: project.unitTestRunner.source, included: false },
  'test/aurelia-karma.js'
];

let output = project.platform.output;
let appSrc = project.build.bundles.map(x => path.join(output, x.name));
let entryIndex = appSrc.indexOf(path.join(output, project.build.loader.configTarget));
let entryBundle = appSrc.splice(entryIndex, 1)[0];
let sourceMaps = [{pattern:'scripts/**/*.js.map', included: false}];
let files = [entryBundle].concat(testSrc).concat(appSrc).concat(sourceMaps);

let transpilerOptions = project.transpiler.options;
transpilerOptions.sourceMap = 'inline';

const isDocker = require('is-docker')();

module.exports = function(config) {
  config.set({
    basePath: '',
    frameworks: [project.testFramework.id],
    customLaunchers: {
      ChromeCustom: {
        base: 'ChromeHeadless',
        // We must disable the Chrome sandbox when running Chrome inside Docker (Chrome's sandbox needs
        // more permissions than Docker allows by default)
        flags: isDocker ? ['--no-sandbox'] : []
      }
    },
    files: files,
    exclude: [],
    preprocessors: {
      [project.unitTestRunner.source]: [project.transpiler.id],
      [appSrc]: ['sourcemap']
    },
    'babelPreprocessor': { options: transpilerOptions },
    reporters: ['progress'],
    port: 9876,
    colors: true,
    logLevel: config.LOG_DEBUG,
    autoWatch: true,
    browsers: ['ChromeCustom'],
    singleRun: false,
    // client.args must be a array of string.
    // Leave 'aurelia-root', project.paths.root in this order so we can find
    // the root of the aurelia project.
    client: {
      args: ['aurelia-root', project.paths.root]
    }
  });
};
