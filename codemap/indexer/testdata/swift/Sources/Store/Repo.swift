import Orders

public protocol Repo {
    func save(_ order: Order) -> Int
}

public final class PgRepo: Repo {
    public static var table: String { "orders" }

    public init() {}

    public func save(_ order: Order) -> Int { insert(order) }

    func insert(_ order: Order) -> Int { order.id }
}
