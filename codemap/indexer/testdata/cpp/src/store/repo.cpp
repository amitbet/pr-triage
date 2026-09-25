#include "repo.hpp"

namespace acme::store {

PgRepo::PgRepo() {}

int PgRepo::save(const Order& o) { return insert(o); }

const char* PgRepo::table() { return "orders"; }

int PgRepo::insert(const Order& o) { return order_id(&o); }

}  // namespace acme::store
