package acme.store

import acme.orders.Order

trait Repo {
  def save(order: Order): Int
}

class PgRepo extends Repo {
  override def save(order: Order): Int = insert(order)
  def insert(order: Order): Int = order.id
}

object PgRepo {
  val table = "orders"
}
