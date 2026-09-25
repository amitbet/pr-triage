package acme.orders

class OrderServiceTest {
    fun places() = OrderService().place(Order(1))
}
