import Foundation
import Store

/// Places orders.
public final class OrderService {
    private let repo: Repo

    public init(repo: Repo = PgRepo()) {
        self.repo = repo
    }

    public func place(_ order: Order) -> Int {
        guard validate(order) else { return 0 }
        let pg = PgRepo()
        _ = pg.insert(order)
        return repo.save(order) + PgRepo.table.count
    }

    private func validate(_ order: Order) -> Bool { order.id > 0 }
}

public func run() -> Int { OrderService().place(Order(id: 1)) }
