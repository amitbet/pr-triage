public struct Order {
    public let id: Int
}

extension Order {
    var valid: Bool { id > 0 }
}
