const { save } = require('./lib/store');
const store = require('./lib/store');

function handler(req) {
  save(req);
  return store.save(req);
}

module.exports = handler;
