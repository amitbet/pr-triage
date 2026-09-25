package acme.store

import acme.orders.Order

interface Repo {
    fun save(o: Order): Int
}

class PgRepo : Repo {
    override fun save(o: Order): Int = insert(o)

    fun insert(o: Order): Int = o.id

    companion object {
        fun table() = "orders"
    }
}
