import 'package:shop/src/store/repo.dart';
import 'util.dart' as util;

/// Places orders.
class OrderService {
  final Repo repo;

  OrderService(this.repo);

  factory OrderService.pg() => OrderService(PgRepo());

  int place(Order order) {
    if (!_validate(order)) {
      return 0;
    }
    util.clean(order);
    return repo.save(order) + PgRepo.table.length;
  }

  bool _validate(Order order) => order.id > 0;
}

class Order {
  final int id;
  const Order(this.id);
}

int run() => OrderService.pg().place(const Order(1));
