#include "order.h"

static int checked(int id) { return id > 0 ? id : 0; }

int order_id(const Order *o) {
  if (o == 0 || o->id < 0) {
    return -1;
  }
  return checked(o->id);
}
