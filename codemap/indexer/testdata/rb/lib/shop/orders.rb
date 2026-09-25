require_relative 'store/repo'
require_relative 'util'

module Shop
  # Places orders.
  class OrderService
    def initialize(repo = Store::PgRepo.new)
      @repo = repo
      @audit = Audit.new
    end

    def place(order)
      return 0 unless valid?(order)
      Util.clean(order)
      @audit.record(order)
      @repo.save(order) + Store::PgRepo.table.size
    end

    private

    def valid?(order)
      order && order.id.positive?
    end
  end

  class Audit
    def record(order) = order
  end
end

def main
  Shop::OrderService.new.place(1)
end
