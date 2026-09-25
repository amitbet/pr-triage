package acme.orders

import acme.store.PgRepo
import acme.store.Repo
import acme.util.*

/** Places orders. */
class OrderService(private val repo: Repo) {
    constructor() : this(PgRepo())

    fun place(o: Order): Int {
        if (!validate(o)) {
            return 0
        }
        val pg = PgRepo()
        pg.insert(o)
        clean(o.id)
        return repo.save(o) + PgRepo.table().length
    }

    private fun validate(o: Order) = o.id > 0
}

data class Order(val id: Int)

fun run() = OrderService().place(Order(1))
