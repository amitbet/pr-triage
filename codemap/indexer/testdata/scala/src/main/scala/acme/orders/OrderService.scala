package acme.orders

import acme.store.{PgRepo, Repo}
import acme.util._

/** Places orders. */
class OrderService(repo: Repo) {
  def place(order: Order): Int = {
    if (!validate(order)) return 0
    val pg = new PgRepo()
    pg.insert(order)
    clean(order)
    repo.save(order) + PgRepo.table.length
  }

  private def validate(order: Order): Boolean = order.id > 0
}

object OrderService {
  def apply(): OrderService = new OrderService(new PgRepo())
}

case class Order(id: Int)

object Main {
  def run(): Int = OrderService().place(Order(1))
}
