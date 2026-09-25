#pragma once
#include "order.h"

namespace acme::store {

class Repo {
 public:
  virtual ~Repo() = default;
  virtual int save(const Order& o) = 0;
};

class PgRepo : public Repo {
 public:
  PgRepo();
  int save(const Order& o) override;
  static const char* table();

 private:
  int insert(const Order& o);
};

}  // namespace acme::store
