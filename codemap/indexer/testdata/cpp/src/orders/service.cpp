#include "store/repo.hpp"

namespace acme {

class OrderService {
 public:
  explicit OrderService(store::Repo* repo) : repo_(repo) {}

  int place(const Order& o) {
    if (!validate(o)) return 0;
    store::PgRepo pg;
    pg.save(o);
    return repo_->save(o) + store::PgRepo::table()[0];
  }

 private:
  bool validate(const Order& o) { return order_id(&o) > 0; }
  store::Repo* repo_;
};

int run() {
  store::PgRepo pg;
  OrderService svc(&pg);
  return svc.place(Order{1});
}

}  // namespace acme
