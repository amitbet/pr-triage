const Db = require('./db');

function save(x) {
  return new Db().query(x);
}

exports.save = save;
