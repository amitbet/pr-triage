import '../order_service.dart';

abstract class Repo {
  int save(Order order);
}

class PgRepo implements Repo {
  static const table = 'orders';

  @override
  int save(Order order) => _insert(order);

  int _insert(Order order) => order.id;
}
